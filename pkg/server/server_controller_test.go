// Copyright 2022 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package server

import (
	"context"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/testutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
)

func TestServerController(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()

	s, db, _ := serverutils.StartServer(t, base.TestServerArgs{
		DisableDefaultTestTenant: true,
	})
	defer s.Stopper().Stop(ctx)

	ts := s.(*TestServer)

	d, err := ts.serverController.getServer(ctx, "system")
	require.NoError(t, err)
	if d.(*systemServerWrapper).server != ts.Server {
		t.Fatal("expected wrapped system server")
	}

	d, err = ts.serverController.getServer(ctx, "somename")
	require.Nil(t, d)
	require.Error(t, err, `no tenant found with name "somename"`)

	_, err = db.Exec("CREATE TENANT hello; ALTER TENANT hello START SERVICE SHARED")
	require.NoError(t, err)

	_, err = ts.serverController.getServer(ctx, "hello")
	// TODO(knz): We're not really expecting an error here.
	// The actual error seen will exist as long as in-memory
	// servers use the standard KV connector.
	//
	// To make this error go away, we need either to place
	// this test in a separate CCL package, or to make these servers
	// use a new non-CCL connector.
	//
	// However, none of this is necessary to test the
	// controller itself: it's sufficient to see that the
	// tenant constructor was called.
	require.Error(t, err, "tenant connector requires a CCL binary")
	// TODO(knz): test something about d
}

func TestSQLErrorUponInvalidTenant(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()

	s, sqlDB, _ := serverutils.StartServer(t, base.TestServerArgs{
		DisableDefaultTestTenant: true,
	})
	defer s.Stopper().Stop(ctx)

	sqlAddr := s.ServingSQLAddr()
	db, err := serverutils.OpenDBConnE(sqlAddr, "cluster:nonexistent", false, s.Stopper())
	// Expect no error yet: the connection is opened lazily; an
	// error here means the parameters were incorrect.
	require.NoError(t, err)

	err = db.Ping()
	require.Regexp(t, `service unavailable for target tenant \(nonexistent\)`, err.Error())

	// Regression test for CRDB-40449; make sure pre-conn memory is freed.
	testutils.SucceedsSoon(t, func() error {
		var usedPreConnMemory int
		err = sqlDB.QueryRow("select used from crdb_internal.node_memory_monitors where name='pre-conn'").Scan(&usedPreConnMemory)
		if err != nil {
			return err
		}
		if usedPreConnMemory != 0 {
			return errors.Errorf("expected 0 bytes used, got %d", usedPreConnMemory)
		}
		return nil
	})
}
