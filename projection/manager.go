package projection

type Event struct {
	Type    string
	Payload []byte
}

type Projection interface {
	Name() string
	Handle(event Event) error
}

type Manager struct {
	projections []Projection
}

func NewManager() *Manager {
	return &Manager{
		projections: []Projection{},
	}
}

func (m *Manager) Register(p Projection) {
	m.projections = append(m.projections, p)
}

func (m *Manager) Apply(event Event) error {
	for _, p := range m.projections {
		err := p.Handle(event)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Dispatch(evt Event) error {
	for _, p := range m.projections {
		if err := p.Handle(evt); err != nil {
			return err
		}
	}
	return nil
}
