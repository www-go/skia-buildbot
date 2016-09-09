package db

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"go.skia.org/infra/go/util"

	"github.com/satori/go.uuid"
	"github.com/skia-dev/glog"
)

type inMemoryTaskDB struct {
	CommentBox
	tasks    map[string]*Task
	tasksMtx sync.RWMutex
	modTasks ModifiedTasks
}

// See docs for TaskDB interface. Does not take any locks.
func (db *inMemoryTaskDB) AssignId(t *Task) error {
	if t.Id != "" {
		return fmt.Errorf("Task Id already assigned: %v", t.Id)
	}
	t.Id = uuid.NewV5(uuid.NewV1(), uuid.NewV4().String()).String()
	return nil
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) Close() error {
	return nil
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) GetTaskById(id string) (*Task, error) {
	db.tasksMtx.RLock()
	defer db.tasksMtx.RUnlock()
	if task := db.tasks[id]; task != nil {
		return task.Copy(), nil
	}
	return nil, nil
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) GetTasksFromDateRange(start, end time.Time) ([]*Task, error) {
	db.tasksMtx.RLock()
	defer db.tasksMtx.RUnlock()

	rv := []*Task{}
	// TODO(borenet): Binary search.
	for _, b := range db.tasks {
		if (b.Created.Equal(start) || b.Created.After(start)) && b.Created.Before(end) {
			rv = append(rv, b.Copy())
		}
	}
	sort.Sort(TaskSlice(rv))
	return rv, nil
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) GetModifiedTasks(id string) ([]*Task, error) {
	return db.modTasks.GetModifiedTasks(id)
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) PutTask(task *Task) error {
	db.tasksMtx.Lock()
	defer db.tasksMtx.Unlock()

	if util.TimeIsZero(task.Created) {
		return fmt.Errorf("Created not set. Task %s created time is %s. %v", task.Id, task.Created, task)
	}

	if task.Id == "" {
		if err := db.AssignId(task); err != nil {
			return err
		}
	} else if existing := db.tasks[task.Id]; existing != nil {
		if !existing.DbModified.Equal(task.DbModified) {
			glog.Warningf("Cached Task has been modified in the DB. Current:\n%v\nCached:\n%v", existing, task)
			return ErrConcurrentUpdate
		}
	}
	task.DbModified = time.Now()

	// TODO(borenet): Keep tasks in a sorted slice.
	db.tasks[task.Id] = task
	db.modTasks.TrackModifiedTask(task)
	return nil
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) PutTasks(tasks []*Task) error {
	for _, t := range tasks {
		if err := db.PutTask(t); err != nil {
			return err
		}
	}
	return nil
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) StartTrackingModifiedTasks() (string, error) {
	return db.modTasks.StartTrackingModifiedTasks()
}

// See docs for TaskDB interface.
func (db *inMemoryTaskDB) StopTrackingModifiedTasks(id string) {
	db.modTasks.StopTrackingModifiedTasks(id)
}

// NewInMemoryTaskDB returns an extremely simple, inefficient, in-memory TaskDB
// implementation.
func NewInMemoryTaskDB() TaskAndCommentDB {
	db := &inMemoryTaskDB{
		tasks: map[string]*Task{},
	}
	return db
}
