package watch

type Ably struct {
	QpointID string
	Token    string
}

func (a *Ably) Watch() (chan string, error) {
	return make(chan string), nil
}

func (a *Ably) Stop() error {
	return nil
}
