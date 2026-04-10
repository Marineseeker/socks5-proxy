package user

import (
	"sync"
	"sync/atomic"
)

type User struct {
	ID       uint64
	Username string
	Password string
	Email    string

	flow *Flow
}

type Flow struct {
	upload   uint64
	download uint64
}

func NewUser(username, password string) *User {
	return &User{
		Username: username,
		Password: password,
		flow:     &Flow{},
	}
}

var (
	mu          sync.RWMutex
	currentUser *User
)

// SetCurrentUser sets the package-level current user used by GetCurrentUser.
func SetCurrentUser(u *User) {
	mu.Lock()
	currentUser = u
	mu.Unlock()
}

// GetCurrentUser returns a singleton current user. If none is set yet,
// it initializes a default user (Marine) and returns it.
func GetCurrentUser() *User {
	mu.RLock()
	u := currentUser
	mu.RUnlock()
	if u != nil {
		return u
	}

	mu.Lock()
	defer mu.Unlock()
	if currentUser == nil {
		currentUser = &User{
			ID:       1,
			Username: "Marine",
			Password: "1235689412",
			Email:    "",
			flow:     &Flow{},
		}
	}
	return currentUser
}

func (u *User) AddUpload(n uint64)   { atomic.AddUint64(&u.flow.upload, n) }
func (u *User) AddDownload(n uint64) { atomic.AddUint64(&u.flow.download, n) }
func (u *User) GetFlow() (upload, download uint64) {
	return atomic.LoadUint64(&u.flow.upload), atomic.LoadUint64(&u.flow.download)
}
