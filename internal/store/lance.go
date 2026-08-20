package store

import (
	"context"

	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"
)

type Lance struct {
	conn contracts.IConnection
}

func OpenLance(ctx context.Context, path string) (Store, error) {
	conn, err := lancedb.Connect(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return &Lance{conn: conn}, nil
}

func (l *Lance) Close() error {
	return l.conn.Close()
}
