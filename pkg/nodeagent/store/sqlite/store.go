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

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store"
	_ "github.com/mattn/go-sqlite3"
	klog "k8s.io/klog/v2"
)

type TextFromObjectFunc func(interface{}) (string, error)
type TextToObjectFunc func(string) (interface{}, error)

var _ store.Store = &sqLiteStore{}

type sqLiteStore struct {
	options            *SQLiteStoreOptions
	DB                 *sql.DB
	Table              string
	TextToObjectFunc   TextToObjectFunc
	TextFromObjectFunc TextFromObjectFunc
}

// ListObject implements store.Store
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
		if obj, err = s.TextToObjectFunc(text); err != nil {
			return nil, err
		} else {
			objs = append(objs, obj)
		}
	}
	return objs, nil
}

// DelObject implements store.Store
func (s *sqLiteStore) DelObject(identifier string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	stmt, err := s.DB.Prepare(fmt.Sprintf("delete from %s where identifier = ?", s.Table))
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(identifier)
	tx.Commit()

	if err != nil {
		return err
	}
	return nil
}

// PutObject implements Store
func (s *sqLiteStore) PutObject(identifier string, obj interface{}) error {
	var sqlobjtext string
	var err error
	if sqlobjtext, err = s.TextFromObjectFunc(obj); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(fmt.Sprintf("insert or replace into %s(identifier, content) values(?, ?)", s.Table))
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(identifier, sqlobjtext)
	if err != nil {
		return err
	}
	tx.Commit()

	return nil
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
		return nil, store.StoreObjectNotFound
	}

	var obj interface{}
	if obj, err = s.TextToObjectFunc(text); err != nil {
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
		klog.ErrorS(err, "connect sqlite failed")
		return err
	}
	s.DB = db
	return nil
}

func (s *sqLiteStore) disconnect() error {
	return s.DB.Close()
}

func (s *sqLiteStore) initTable() error {
	sqlStmt := fmt.Sprintf("create table if not exists %s (identifier varchar(36) primary key, content text)", s.Table)
	_, err := s.DB.Exec(sqlStmt)
	if err != nil {
		klog.ErrorS(err, "failed to create table", "table", s.Table)
		return err
	}

	return nil
}

func NewSqliteStore(table string, options *SQLiteStoreOptions, toObjectFunc TextToObjectFunc, fromObjectFunc TextFromObjectFunc) (*sqLiteStore, error) {
	store := &sqLiteStore{
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
