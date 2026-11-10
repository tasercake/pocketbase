package hdrthumb

// TilePyramid holds display-oriented dimensions reported by the native worker.
type TilePyramid struct {
	Width         int `json:"width"`
	Height        int `json:"height"`
	PreviewWidth  int `json:"previewWidth"`
	PreviewHeight int `json:"previewHeight"`
}
