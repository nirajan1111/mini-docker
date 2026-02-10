package image

// Image represents a pulled container image with all its layer information.
type Image struct {
	Name   string
	Tag    string
	Layers []Layer
}

// Layer represents a single filesystem layer from a container image.
type Layer struct {
	Digest    string // sha256 digest from the manifest
	MediaType string
	Size      int64
}
