package download

// bundle downloader
type Downloader interface {
	Fetch(version string) (string, error)
}
