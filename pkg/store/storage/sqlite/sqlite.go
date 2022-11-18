/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"centaurusinfra.io/fornax-serverless/pkg/store/storage"
	"github.com/mattn/go-sqlite3"
)

type sqLiteStore struct {
	mu                 sync.Mutex
	options            *SQLiteStoreOptions
	DB                 *sql.DB
	Table              string
	TextToObjectFunc   storage.TextToObjectFunc
	TextFromObjectFunc storage.TextFromObjectFunc
	insStmt            *sql.Stmt
	udpStmt            *sql.Stmt
	delStmt            *sql.Stmt
}

func (s *sqLiteStore) ListObject() ([]interface{}, error) {
	row, err := s.DB.Query(fmt.Sprintf("select identifier, content from %s", s.Table))
	if err != nil {
		return nil, err
	}

	objs := []interface{}{}
	defer row.Close()

	for row.Next() { // Iterate and fetch the records from result cursor
		var id string
		var text string
		err := row.Scan(&id, &text)
		if err != nil {
			return nil, err
		}

		var obj interface{}
		if obj, err = s.TextToObjectFunc([]byte(text)); err != nil {
			return nil, err
		} else {
			objs = append(objs, obj)
		}
	}
	return objs, nil
}

func (s *sqLiteStore) DelObject(identifier string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	stmt, err := s.getDelStmt()
	if err != nil {
		return err
	}

	_, err = stmt.Exec(identifier)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()

	return nil
}

func (s *sqLiteStore) PutObject(identifier string, obj interface{}, revision int64) error {
	var sqlobjtext []byte
	var err error
	if sqlobjtext, err = s.TextFromObjectFunc(obj); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}

	stmt, err := s.getInsStmt()
	if err != nil {
		return err
	}
	_, err = stmt.Exec(identifier, revision, sqlobjtext)
	if err != nil {
		if liteErr, ok := err.(sqlite3.Error); ok {
			if liteErr.ExtendedCode != sqlite3.ErrConstraintPrimaryKey {
				tx.Rollback()
				return err
			}
		} else {
			tx.Rollback()
			return err
		}
	}

	stmt, err = s.getUdpStmt()
	if err != nil {
		return err
	}
	_, err = stmt.Exec(identifier, revision, sqlobjtext, identifier, revision)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()

	return nil
}

func (s *sqLiteStore) getInsStmt() (*sql.Stmt, error) {
	if s.insStmt != nil {
		return s.insStmt, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, err := s.DB.Prepare(fmt.Sprintf("insert into %s(identifier, revision, content) values(?, ?, ?)", s.Table))
	if err != nil {
		return nil, err
	}
	s.insStmt = stmt
	return stmt, err
}

func (s *sqLiteStore) getUdpStmt() (*sql.Stmt, error) {
	if s.udpStmt != nil {
		return s.udpStmt, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, err := s.DB.Prepare(fmt.Sprintf("update %s set identifier = ?, revision =?, content =? where identifier = ? and revision <= ?", s.Table))
	if err != nil {
		return nil, err
	}
	s.udpStmt = stmt
	return stmt, err
}

func (s *sqLiteStore) getDelStmt() (*sql.Stmt, error) {
	if s.delStmt != nil {
		return s.delStmt, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, err := s.DB.Prepare(fmt.Sprintf("delete from %s where identifier = ?", s.Table))
	if err != nil {
		return nil, err
	}
	s.delStmt = stmt
	return stmt, err
}

func (s *sqLiteStore) GetObject(identifier string) (interface{}, error) {
	stmt, err := s.DB.Prepare(fmt.Sprintf("select identifier, content from %s where identifier = ?", s.Table))
	defer stmt.Close()
	row, err := stmt.Query(identifier)
	if err != nil {
		return nil, err
	}

	defer row.Close()
	numOfRow := 0

	var id string
	var text string
	for row.Next() { // Iterate and fetch the records from result cursor
		numOfRow += 1
		if numOfRow > 1 {
			return nil, fmt.Errorf("identifier %s has multiple objects", identifier)
		}
		err := row.Scan(&id, &text)
		if err != nil {
			return nil, err
		}
	}
	if numOfRow == 0 {
		return nil, storage.ObjectNotFound
	}

	var obj interface{}
	if obj, err = s.TextToObjectFunc([]byte(text)); err != nil {
		return nil, err
	}
	return obj, nil
}

type SQLiteStoreOptions struct {
	ConnUrl string
}

func (s *sqLiteStore) connect() error {
	db, err := sql.Open("sqlite3", s.options.ConnUrl)
	if err != nil {
		return fmt.Errorf("Failed to Connect sqlite %s, cause %v", s.options.ConnUrl, err)
	}
	s.DB = db
	return nil
}

func (s *sqLiteStore) disconnect() error {
	return s.DB.Close()
}

func (s *sqLiteStore) initTable() error {
	sqlStmt := fmt.Sprintf("create table if not exists %s (identifier varchar(36) primary key, revision integer, content text)", s.Table)
	_, err := s.DB.Exec(sqlStmt)
	if err != nil {
		return fmt.Errorf("Failed to create sqlite table %s, cause %v", s.Table, err)
	}

	return nil
}

func NewSqliteStore(table string, options *SQLiteStoreOptions, toObjectFunc storage.TextToObjectFunc, fromObjectFunc storage.TextFromObjectFunc) (*sqLiteStore, error) {
	store := &sqLiteStore{
		mu:      sync.Mutex{},
		options: options,
	}
	store.Table = table
	if toObjectFunc == nil {
		return nil, errors.New("TextToObject func is not provided to NewSqliteStore")
	} else {
		store.TextToObjectFunc = toObjectFunc
	}

	if fromObjectFunc == nil {
		return nil, errors.New("TextFromObject func is not provided to NewSqliteStore")
	} else {
		store.TextFromObjectFunc = fromObjectFunc
	}
	err := store.connect()
	if err != nil {
		return nil, err
	}

	err = store.initTable()
	if err != nil {
		return nil, err
	}
	return store, nil
}

var _ storage.Store = &sqLiteStore{}
