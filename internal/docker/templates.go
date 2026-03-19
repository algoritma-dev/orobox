// Package docker provides helpers to generate and run Docker Compose for Orobox.
package docker

import "io/fs"

// Templates holds the embedded filesystem for docker-related templates.
var Templates fs.FS
