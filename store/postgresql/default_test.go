package postgresql

import (
	"fmt"
	"github.com/polarismesh/polaris/store"
	"testing"
)

func TestInitialize(t *testing.T) {
	conf := &store.Config{
		Name: "Postgresql",
		Option: map[string]interface{}{
			"master": map[interface{}]interface{}{
				"dbType": "postgres",
				"dbUser": "postgres",
				"dbPwd":  "aaaaaa",
				"dbAddr": "192.168.31.19",
				"dbPort": "5432",
				"dbName": "polaris_server",

				"maxOpenConns":     10,
				"maxIdleConns":     10,
				"connMaxLifetime":  10,
				"txIsolationLevel": 10,
			},
			"slave": map[interface{}]interface{}{
				"dbType": "postgres",
				"dbUser": "postgres",
				"dbPwd":  "aaaaaa",
				"dbAddr": "192.168.31.19",
				"dbPort": "5432",
				"dbName": "polaris_server",

				"maxOpenConns":     10,
				"maxIdleConns":     10,
				"connMaxLifetime":  10,
				"txIsolationLevel": 10,
			},
		},
	}
	obj := &postgresqlStore{}
	err := obj.Initialize(conf)
	fmt.Println(err)
}
