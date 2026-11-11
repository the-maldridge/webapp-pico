// Command fetchpico vendors the Pico CSS bundle into this repository.
//
// It exists because Pico is distributed as an npm package and a Go build has no
// npm. The package is a plain gzipped tar with a "package/" prefix, so the whole
// job is one HTTP GET, one tar walk, and one write, all of which are in the
// standard library. Nothing here needs a toolchain beyond Go.
//
// What it writes is committed, and this program is how that committed bundle is
// reproduced rather than trusted. The lock file records the digest of every byte
// the program is allowed to place on disk and of the tarball they came from, so
// advancing a release means changing a version, running the program, and reading
// the diff it produces; a bundle that does not match its own lock is a test
// failure, and the check runs with no network so a build can ask the question
// without fetching anything.
//
// The lock file is the single source of truth for the version. There is no
// version constant in this program to drift out of step with it, which is the
// only reason the two would ever disagree.
//
// Usage:
//
//	go run ./internal/fetchpico               // fetch, verify, write
//	go run ./internal/fetchpico -check        // verify what is on disk, write nothing
//	go run ./internal/fetchpico -version 2.2.0 -digest // report digests without writing
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// lockFile names the manifest this program verifies against. It is committed and
// is the only artifact of this program that is.
const lockFile = "pico.lock"

// urlFormat builds the npm registry's tarball address for a version. The registry
// serves plain tarballs at this path with no authentication, no redirect, and no
// content negotiation, which is what makes a stdlib client sufficient.
const urlFormat = "https://registry.npmjs.org/@picocss/pico/-/pico-%s.tgz"

// maxBundleBytes bounds what will be read from the network. The extracted bundle
// is well under 100KB, but the package tarball is around 2.7MB because it also
// carries SCSS sources, a classless variant, and the theme and colour templates,
// and this program reads the whole archive to find the two members it wants. The
// bound is therefore well above the archive rather than the extracted file, since
// the number that has to be meaningful is the one a remote host could exceed.
const maxBundleBytes = 16 << 20

// fileMap maps a path inside the npm package to where it lands here. Only the
// minified bundle and the licence are wanted: the SCSS would mean shipping a Sass
// compiler to consume, the classless build is a different framework's opinion
// about markup this theme does not share, and the point of vendoring a CSS
// framework into a Go binary is that nothing has to be built at runtime.
var fileMap = map[string]string{
	"package/css/pico.min.css": "static/css/pico.min.css",
	"package/LICENSE.md":       "static/css/LICENSE.md",
}

func main() {
	version := flag.String("version", "", "Pico version to fetch (default: the version in "+lockFile+")")
	check := flag.Bool("check", false, "verify what is already on disk and write nothing")
	digest := flag.Bool("digest", false, "print digests for the fetched bundle and exit")
	list := flag.Bool("list", false, "list the package contents and exit")
	flag.Parse()

	if err := run(*version, *check, *digest, *list); err != nil {
		fmt.Fprintln(os.Stderr, "fetchpico:", err)
		os.Exit(1)
	}
}

func run(version string, check, digest, list bool) error {
	lock, err := readLock(lockFile)
	if err != nil {
		return err
	}
	if version == "" {
		version = lock.version
	}
	if version == "" {
		return fmt.Errorf("%s declares no version and none was given", lockFile)
	}

	// Checking is offline by design. Its question is "is what is already here the
	// thing the manifest describes", and fetching a 2.7 MB tarball to answer it
	// would make the one operation a build needs to be able to do without the
	// network depend on having the network. It therefore only makes sense about
	// the locked version, and says so rather than checking something else.
	if check {
		if version != lock.version {
			return fmt.Errorf("--check verifies what is on disk against %s, which pins %s, not %s",
				lockFile, lock.version, version)
		}
		return checkOnDisk(lock, version)
	}

	// A version other than the locked one is a report, never a write: silently
	// replacing the pinned bundle by remembering a flag would make the lock file
	// a suggestion rather than a record.
	reportOnly := version != lock.version || digest || list

	body, err := download(version)
	if err != nil {
		return err
	}

	tarSum := sha256.Sum256(body)
	if lock.tarball != "" && version == lock.version {
		if got := hex.EncodeToString(tarSum[:]); got != lock.tarball {
			return fmt.Errorf("the tarball for %s has digest %s but %s says %s; refusing to vendor it, "+
				"because the artifact at the registry is not the one that was reviewed",
				version, got, lockFile, lock.tarball)
		}
	}

	files, err := extract(body)
	if err != nil {
		return err
	}

	if list {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("%8d  %s\n", len(files[name]), name)
		}
		return nil
	}

	if digest {
		fmt.Printf("tarball %x\n", tarSum[:])
		for _, src := range sortedKeys(files) {
			sum := sha256.Sum256(files[src])
			fmt.Printf("%s %x\n", src, sum[:])
		}
		return nil
	}

	if err := verifyLock(lock, version, files); err != nil {
		return err
	}

	if reportOnly {
		fmt.Printf("pico %s is not the locked version %s; nothing written. Update %s to vendor it.\n",
			version, lock.version, lockFile)
		return nil
	}

	if err := write(files); err != nil {
		return err
	}
	fmt.Printf("vendored pico %s (%d files)\n", version, len(fileMap))
	return nil
}

// download fetches the package tarball.
//
// The deadline is generous rather than tight because this runs at the top of a
// release update, where a slow mirror is normal, and a hung fetch holding whoever
// is doing the update is much worse than a slow one that finishes.
func download(version string) ([]byte, error) {
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Get(fmt.Sprintf(urlFormat, version))
	if err != nil {
		return nil, fmt.Errorf("could not fetch pico %s: %w", version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pico %s: registry answered %s", version, resp.Status)
	}

	// One byte past the bound, so that a response which is exactly as large as the
	// limit is reported as too large rather than accepted as coincidence.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("could not read pico %s: %w", version, err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("pico %s: registry returned an empty body", version)
	}
	if len(body) > maxBundleBytes {
		return nil, fmt.Errorf("pico %s: registry sent more than %d bytes, which is not the "+
			"artifact this program knows how to vendor", version, maxBundleBytes)
	}
	return body, nil
}

// extract pulls the wanted files out of the gzipped tar.
//
// Every entry is read, not just the wanted ones, because the digest of the file
// has to be computable against what the archive actually contained, and a partial
// read that stopped at the last wanted member would report success on a truncated
// archive.
func extract(body []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("the download is not gzipped: %w", err)
	}
	defer gz.Close()

	found := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		dest, wanted := fileMap[hdr.Name]
		if !wanted {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("%s is not a regular file in the archive", hdr.Name)
		}
		// A member's declared size is honoured only up to the same bound the
		// download has, and a negative one is rejected outright rather than
		// reaching LimitReader, where a negative limit means "no limit" and a
		// corrupt or hostile header would turn into an unbounded read.
		if hdr.Size < 0 || hdr.Size > maxBundleBytes {
			return nil, fmt.Errorf("%s claims %d bytes in the archive, which is not a plausible "+
				"size for it", hdr.Name, hdr.Size)
		}
		// A member declaring its own size gets that much and no more. Reading
		// without the bound would let a malicious or corrupt archive declare a
		// small header and stream arbitrarily many bytes into memory.
		data, err := io.ReadAll(io.LimitReader(tr, hdr.Size+1))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", hdr.Name, err)
		}
		if int64(len(data)) != hdr.Size {
			return nil, fmt.Errorf("%s: archive claims %d bytes, delivered %d", hdr.Name, hdr.Size, len(data))
		}
		found[dest] = data
	}

	for src := range fileMap {
		if _, ok := found[fileMap[src]]; !ok {
			return nil, fmt.Errorf("%s is not in the archive; Pico changed its layout, "+
				"and this program has to be told where the bundle went", src)
		}
	}
	return found, nil
}

// write puts each extracted file where the theme embeds it from.
func write(files map[string][]byte) error {
	for _, dest := range sortedKeys(files) {
		data := files[dest]
		if dir := path.Dir(dest); dir != "." && dir != "/" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("could not create %s: %w", dir, err)
			}
		}
		// 0o644 rather than whatever the archive recorded: an asset is read,
		// never executed, and an executable stylesheet is a finding.
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("could not write %s: %w", dest, err)
		}
	}
	return nil
}

// verifyLock checks each extracted file against the digest recorded for it.
//
// A mismatch is fatal rather than a warning. This is the only thing standing
// between a compromise of the registry mirror or a wrong version pin and a theme
// that ships someone else's CSS to every application embedding it.
func verifyLock(lock lockInfo, version string, files map[string][]byte) error {
	if lock.version != version {
		return nil
	}
	for _, dest := range sortedKeys(files) {
		want, ok := lock.files[dest]
		if !ok {
			// A newly mapped file has no digest yet, which is expected exactly
			// once: the moment the mapping is added. Recording it is the change
			// that makes the next fetch verifiable.
			fmt.Printf("no digest recorded for %s; add one to %s\n", dest, lockFile)
			continue
		}
		got := sha256.Sum256(files[dest])
		if hex.EncodeToString(got[:]) != want {
			return fmt.Errorf("%s has digest %x but %s says %s; refusing to write it",
				dest, got[:], lockFile, want)
		}
	}
	return nil
}

// destinations lists the files this program writes, sorted.
//
// It is the values of fileMap rather than its keys, and the distinction is the
// kind that reads wrong in a hurry: the keys are names inside the archive, and a
// check that stat'd those would be looking for a path that exists only in the
// download.
func destinations() []string {
	out := make([]string, 0, len(fileMap))
	for _, dest := range fileMap {
		out = append(out, dest)
	}
	sort.Strings(out)
	return out
}

// checkOnDisk verifies the vendored files already in place against the manifest,
// without downloading anything.
//
// The check reads the digests rather than merely the names, because "the file
// exists" is satisfied by a truncated download and by an error page saved under
// a stylesheet's name, which are the two failures that produce a working build of
// a broken page. With the bundle committed this is the check a consumer of the
// theme module can run, which is the reason it exists at all.
func checkOnDisk(lock lockInfo, version string) error {
	for _, dest := range destinations() {
		data, err := os.ReadFile(dest)
		if err != nil {
			return fmt.Errorf("%s is missing; run: go generate ./...", dest)
		}
		want, ok := lock.files[dest]
		if !ok {
			return fmt.Errorf("%s has no digest in %s, so it cannot be verified; run go generate ./... "+
				"and record the digest it prints", dest, lockFile)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("%s has digest %s but %s says %s; it is not the reviewed artifact, "+
				"so run go generate ./... rather than editing the manifest", dest, got, lockFile, want)
		}
	}
	fmt.Printf("pico %s matches %s\n", version, lockFile)
	return nil
}

// lockInfo is the parsed manifest.
type lockInfo struct {
	version string
	tarball string
	files   map[string]string
}

// readLock parses the manifest.
//
// The format is key=value lines so that the file is readable in a diff and
// greppable in CI, and because a format this program has to parse but cannot
// comment is a format that will be edited wrong. A malformed line is an error
// rather than a skip, since a lock file that parsed as empty would let every
// verification pass.
func readLock(name string) (lockInfo, error) {
	lock := lockInfo{files: map[string]string{}}

	data, err := os.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return lock, fmt.Errorf("%s is missing; it records which Pico release is vendored and is not generated", name)
	}
	if err != nil {
		return lock, fmt.Errorf("could not read %s: %w", name, err)
	}

	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return lock, fmt.Errorf("%s line %d: %q is not key=value", name, i+1, line)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)

		switch {
		case key == "version":
			lock.version = value
		case key == "tarball_sha256":
			lock.tarball = value
		case strings.HasPrefix(key, "sha256:"):
			lock.files[strings.TrimPrefix(key, "sha256:")] = value
		default:
			return lock, fmt.Errorf("%s line %d: unknown key %q", name, i+1, key)
		}
	}
	if lock.version == "" {
		return lock, fmt.Errorf("%s declares no version", name)
	}
	return lock, nil
}

// sortedKeys makes output deterministic.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
