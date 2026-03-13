package projection

import "mini-database/storage"

type Projection interface {
	Handle(event storage.Event) error
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

func (m *Manager) Dispatch(evt storage.Event) error {
	for _, p := range m.projections {
		if err := p.Handle(evt); err != nil {
			return err
		}

	}

	return nil
}
