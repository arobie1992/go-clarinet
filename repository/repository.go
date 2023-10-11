package repository

import (
	"sync"

	"github.com/go-clarinet/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var db *gorm.DB
var once sync.Once

func GetDB() *gorm.DB {
	return db
}

func InitDB(config *config.Config, dst ...interface{}) error {
	var retErr error = nil
	once.Do(func() {
		// use a tempDB variable so the shared variable is only set if there are no errors
		// shouldn't really matter a ton since the app should shut down if there are errors
		// but this doesn't hurt
		tempDB, err := gorm.Open(sqlite.Open(config.Libp2p.DbPath), &gorm.Config{})
		if err != nil {
			retErr = err
			return
		}
		// add persistence classes here
		if err := tempDB.AutoMigrate(dst...); err != nil {
			retErr = err
			return
		}
		db = tempDB
	})
	return retErr
}
