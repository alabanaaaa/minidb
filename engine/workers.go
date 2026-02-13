package engine

type Worker struct {
	ID     string
	Name   string
	Active bool
}

type WorkerService struct {
	workers map[string]*Worker
}

func NewWorkerService() *WorkerService {
	return &WorkerService{
		workers: make(map[string]*Worker),
	}
}

func (w *WorkerService) AddWorker(id, name string) {
	w.workers[id] = &Worker{
		ID:     id,
		Name:   name,
		Active: true,
	}
}

func (w *WorkerService) GetWorker(id string) (*Worker, bool) {
	worker, exists := w.workers[id]
	return worker, exists
}
