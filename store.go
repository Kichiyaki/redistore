package redistore

import (
	"net/http"

	"github.com/gorilla/sessions"
)

//Store implements gorilla/sessions.Store interface and adds new functionality.
type Store interface {
	SetKeyPrefix(p string) Store
	SetOptions(opts *sessions.Options) Store
	SetMaxLength(length int) Store
	Get(r *http.Request, name string) (*sessions.Session, error)
	New(r *http.Request, name string) (*sessions.Session, error)
	Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error
	Update(session *sessions.Session) error
	Delete(r *http.Request, w http.ResponseWriter, session *sessions.Session) error
	DeleteByID(ids ...string) error
	GetAll() ([]*sessions.Session, error)
	Client() Client
	Options() *sessions.Options
	MaxLength() int
	KeyPrefix() string
}
