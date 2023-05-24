package proxy

type TcpProxy struct {
	Listen string
}

func (p *TcpProxy) Start() error {
	return nil
}

func (p *TcpProxy) Forward(to string) error {
	return nil
}

func (p *TcpProxy) Stop() error {
	return nil
}
