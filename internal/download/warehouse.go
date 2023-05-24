package download

type Warehouse struct {
	QpointID string
	Token    string
	DataDir  string
}

func (w *Warehouse) Fetch(version string) (string, error) {
	return "", nil
}
