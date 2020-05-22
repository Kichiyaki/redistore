// Copyright 2012 Brian "bojo" Jones. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package redistore

import (
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-redis/redis/v7"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

type RediStore struct {
	client     redis.UniversalClient
	codecs     []securecookie.Codec
	options    *sessions.Options
	serializer SessionSerializer
	maxLength  int
	keyPrefix  string
}

// NewRedisStore returns a new RedisStore.
func NewRedisStore(client redis.UniversalClient, keyPrefix string, keyPairs ...[]byte) (*RediStore, error) {
	store := &RediStore{
		client: client,
		codecs: securecookie.CodecsFromPairs(keyPairs...),
		options: &sessions.Options{
			Path:   "/",
			MaxAge: 4096,
		},
		serializer: JSONSerializer{},
		maxLength:  4096,
		keyPrefix:  keyPrefix,
	}
	_, err := store.ping()
	return store, err
}

// SetKeyPrefix set the prefix
func (s *RediStore) SetKeyPrefix(p string) *RediStore {
	s.keyPrefix = p
	return s
}

// SetOptions set the session options
func (s *RediStore) SetOptions(opts *sessions.Options) *RediStore {
	s.options = opts
	return s
}

// SetMaxLength sets RediStore.maxLength if the `l` argument is greater or equal 0
// maxLength restricts the maximum length of new sessions to l.
// If l is 0 there is no limit to the size of a session, use with caution.
// The default for a new RediStore is 4096. Redis allows for max.
// value sizes of up to 512MB (http://redis.io/topics/data-types)
// Default: 4096,
func (s *RediStore) SetMaxLength(length int) *RediStore {
	s.maxLength = length
	return s
}

// SetSerializer sets the serializer.
func (s *RediStore) SetSerializer(serializer SessionSerializer) *RediStore {
	s.serializer = serializer
	return s
}

// Client returns the Client.
func (s *RediStore) Client() redis.UniversalClient {
	return s.client
}

// Client returns the *sessions.Options.
func (s *RediStore) Options() *sessions.Options {
	return s.options
}

// Client returns the *sessions.Options.
func (s *RediStore) MaxLength() int {
	return s.maxLength
}

// Client returns the prefix.
func (s *RediStore) KeyPrefix() string {
	return s.keyPrefix
}

// Get returns a session for the given name after adding it to the registry.
//
// See gorilla/sessions FilesystemStore.Get().
func (s *RediStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New returns a session for the given name without adding it to the registry.
//
// See gorilla/sessions FilesystemStore.New().
func (s *RediStore) New(r *http.Request, name string) (*sessions.Session, error) {
	var (
		err error
		ok  bool
	)
	session := sessions.NewSession(s, name)
	// make a copy
	options := *s.options
	session.Options = &options
	session.IsNew = true
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, s.codecs...)
		if err == nil {
			ok, err = s.load(session)
			session.IsNew = !(err == nil && ok) // not new if no error and data available
		}
	}
	return session, err
}

// Save adds a single session to the response.
func (s *RediStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// Marked for deletion.
	if session.Options.MaxAge <= 0 {
		if err := s.client.Do("DEL", s.keyPrefix+session.ID).Err(); err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
	} else {
		// Build an alphanumeric key for the redis store.
		if session.ID == "" {
			session.ID = strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)), "=")
		}
		if err := s.save(session); err != nil {
			return err
		}
		encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, s.codecs...)
		if err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	}
	return nil
}

// Delete removes the session from redis, and sets the cookie to expire.
func (s *RediStore) Delete(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if err := s.client.Del(s.keyPrefix + session.ID).Err(); err != nil {
		return err
	}
	// Set cookie to expire.
	options := *session.Options
	options.MaxAge = -1
	http.SetCookie(w, sessions.NewCookie(session.Name(), "", &options))
	// Clear session values.
	for k := range session.Values {
		delete(session.Values, k)
	}
	return nil
}

// Update updates the session in redis.
func (s *RediStore) Update(session *sessions.Session) error {
	if session.Options.MaxAge <= 0 {
		if err := s.client.Do("DEL", s.keyPrefix+session.ID).Err(); err != nil {
			return err
		}
	} else {
		if session.ID == "" {
			return fmt.Errorf("redistore: invalid session id")
		}
		if err := s.save(session); err != nil {
			return err
		}
	}
	return nil
}

// DeleteByID deletes sessions from redis by id.
func (s *RediStore) DeleteByID(ids ...string) error {
	formattedIds := []string{}
	for _, id := range ids {
		if !strings.Contains(id, s.keyPrefix) {
			formattedIds = append(formattedIds, s.keyPrefix+id)
		} else {
			formattedIds = append(formattedIds, id)
		}
	}
	return s.client.Del(formattedIds...).Err()
}

// GetAll returns all sessions stored in redis.
func (s *RediStore) GetAll() ([]*sessions.Session, error) {
	keys, _, err := s.client.Scan(0, s.keyPrefix+"*", 0).Result()
	if err != nil {
		return nil, err
	}
	results := []*sessions.Session{}
	for _, key := range keys {
		val, err := s.client.Get(key).Result()
		if err != nil {
			return nil, err
		}
		sess := &sessions.Session{
			ID:      strings.Replace(key, s.keyPrefix, "", -1),
			Values:  make(map[interface{}]interface{}),
			Options: s.options,
		}
		err = s.serializer.Deserialize([]byte(val), sess)
		results = append(results, sess)
	}
	return results, nil
}

// ping does an internal ping against a server to check if it is alive.
func (s *RediStore) ping() (bool, error) {
	data, err := s.client.Ping().Result()
	if err != nil {
		return false, err
	}
	return (data == "PONG"), nil
}

// load reads the session from redis.
// returns true if there is a sessoin data in DB
func (s *RediStore) load(session *sessions.Session) (bool, error) {
	data, err := s.client.Get(s.keyPrefix + session.ID).Result()
	if err != nil && err != redis.Nil {
		return false, err
	}
	if err == redis.Nil {
		return false, nil // no data was associated with this key
	}
	return true, s.serializer.Deserialize([]byte(data), session)
}

// save stores the session in redis.
func (s *RediStore) save(session *sessions.Session) error {
	b, err := s.serializer.Serialize(session)
	if err != nil {
		return err
	}
	if s.maxLength != 0 && len(b) > s.maxLength {
		return errors.New("redistore: the value to RediStore is too big")
	}
	age := session.Options.MaxAge
	return s.client.Do("SETEX", s.keyPrefix+session.ID, age, b).Err()
}
