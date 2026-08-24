package main

import (
	"io/fs"
	"net/http"
)

// Static-asset constants keep the public URL and checked-in filesystem root in
// one auditable mapping.
const (
	// staticURLPrefix is the only public URL namespace mapped to checked-in
	// assets. Keeping the trailing slash makes prefix removal unambiguous.
	staticURLPrefix = "/static/"
	// staticDirectory is the repository-relative root used by the executable.
	// The file system below never permits a directory response from this root.
	staticDirectory = "./static"
)

// filesOnlyHTTPFileSystem wraps one rooted HTTP file system and rejects every
// directory. net/http's default file server otherwise generates an HTML index
// that enumerates checked-in asset names when a directory lacks index.html.
type filesOnlyHTTPFileSystem struct {
	root http.FileSystem
}

// Open delegates canonical path resolution to the rooted file system, then
// permits only ordinary files. Stat errors and directories close the acquired
// handle before returning so malformed requests cannot leak descriptors.
func (fileSystem filesOnlyHTTPFileSystem) Open(name string) (http.File, error) {
	file, err := fileSystem.root.Open(name)
	if err != nil {
		return nil, err
	}

	information, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, err
	}
	if !information.Mode().IsRegular() {
		_ = file.Close()

		return nil, fs.ErrNotExist
	}

	return file, nil
}

// newStaticAssetHandler maps only the fixed /static/ namespace to files under
// the fixed asset root. StripPrefix runs before FileServer, while the strict
// file-system wrapper prevents root and nested directory listings.
func newStaticAssetHandler() http.Handler {
	fileServer := http.FileServer(
		filesOnlyHTTPFileSystem{
			root: http.Dir(staticDirectory),
		},
	)

	return http.StripPrefix(staticURLPrefix, fileServer)
}
