// Copyright 2017-2018 New Vector Ltd
// Copyright 2019-2020 The Matrix.org Foundation C.I.C.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sqlite3

import (
	"database/sql"
	"errors"
	"net/url"

	"github.com/matrix-org/dendrite/internal/sqlutil"

	// Import the sqlite3 package
	_ "github.com/mattn/go-sqlite3"

	"github.com/matrix-org/dendrite/eduserver/cache"
	"github.com/matrix-org/dendrite/internal"
	"github.com/matrix-org/dendrite/syncapi/storage/shared"
)

// SyncServerDatasource represents a sync server datasource which manages
// both the database for PDUs and caches for EDUs.
type SyncServerDatasource struct {
	shared.Database
	db *sql.DB
	internal.PartitionOffsetStatements
	streamID streamIDStatements
}

// NewDatabase creates a new sync server database
// nolint: gocyclo
func NewDatabase(dataSourceName string) (*SyncServerDatasource, error) {
	var d SyncServerDatasource
	uri, err := url.Parse(dataSourceName)
	if err != nil {
		return nil, err
	}
	var cs string
	if uri.Opaque != "" { // file:filename.db
		cs = uri.Opaque
	} else if uri.Path != "" { // file:///path/to/filename.db
		cs = uri.Path
	} else {
		return nil, errors.New("no filename or path in connect string")
	}
	if d.db, err = sqlutil.Open(internal.SQLiteDriverName(), cs, nil); err != nil {
		return nil, err
	}
	if err = d.prepare(); err != nil {
		return nil, err
	}
	return &d, nil
}

func (d *SyncServerDatasource) prepare() (err error) {
	if err = d.PartitionOffsetStatements.Prepare(d.db, "syncapi"); err != nil {
		return err
	}
	if err = d.streamID.prepare(d.db); err != nil {
		return err
	}
	accountData, err := NewSqliteAccountDataTable(d.db, &d.streamID)
	if err != nil {
		return err
	}
	events, err := NewSqliteEventsTable(d.db, &d.streamID)
	if err != nil {
		return err
	}
	roomState, err := NewSqliteCurrentRoomStateTable(d.db, &d.streamID)
	if err != nil {
		return err
	}
	invites, err := NewSqliteInvitesTable(d.db, &d.streamID)
	if err != nil {
		return err
	}
	topology, err := NewSqliteTopologyTable(d.db)
	if err != nil {
		return err
	}
	bwExtrem, err := NewSqliteBackwardsExtremitiesTable(d.db)
	if err != nil {
		return err
	}
	d.Database = shared.Database{
		DB:                  d.db,
		Invites:             invites,
		AccountData:         accountData,
		OutputEvents:        events,
		BackwardExtremities: bwExtrem,
		CurrentRoomState:    roomState,
		Topology:            topology,
		EDUCache:            cache.New(),
	}
	return nil
}
