package rdbs

type Track struct {
	Artist string
	Title  string
}

type Playlist struct {
	Name   string
	Tracks []Track
}
