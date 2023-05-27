package pgsql

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"os"
)

func Executl() error {
	Init()
	if PgsqlData != nil {
		//创建mysql连接
		db, err := sql.Open("postgres", fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", PgsqlData.Address, PgsqlData.Port, PgsqlData.Username, PgsqlData.Password, PgsqlData.Database))
		if err != nil {
			return err
		}
		defer db.Close()
		str := fmt.Sprintf("select %s,%s from %s where NAME='%s'", PgsqlData.AccountField, PgsqlData.PwdField, PgsqlData.Table, PgsqlData.Username)
		rows := db.QueryRow(str)
		var name *sql.NullString
		var pwd *sql.NullString
		err = rows.Scan(&name, &pwd)
		if err != nil {
			return err
		}
		if name.String == "" || pwd.String == "" {
			return errors.New("data is null")
		}
		fmt.Println("name:", name.String)
		fmt.Println("passwd:", pwd.String)
		return nil
	}
	return errors.New("open config file failed")
}

func Run() {
	err := Executl()
	if err != nil {
		os.Exit(1)
	}
}
