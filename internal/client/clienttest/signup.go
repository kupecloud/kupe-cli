package clienttest

import (
	"context"
	"sync"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// FakeSignup is an in-memory client.SignupInterface for `kupe user` tests.
type FakeSignup struct {
	mu sync.Mutex

	// DeleteUserErr forces DeleteUser to fail; nil means 204.
	DeleteUserErr error
	// DeleteUserRequests records every body sent.
	DeleteUserRequests []client.DeleteUserRequest
	// Calls records the method name of every call in order.
	Calls []string
}

// NewSignup returns an empty FakeSignup.
func NewSignup() *FakeSignup { return &FakeSignup{} }

// DeleteUser implements client.SignupInterface.
func (f *FakeSignup) DeleteUser(_ context.Context, req client.DeleteUserRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "DeleteUser:"+req.Confirm)
	f.DeleteUserRequests = append(f.DeleteUserRequests, req)
	return f.DeleteUserErr
}

var _ client.SignupInterface = (*FakeSignup)(nil)
