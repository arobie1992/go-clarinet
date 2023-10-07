package repository

import (
	"github.com/go-clarinet/config"
	"github.com/go-clarinet/p2p"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var db *gorm.DB

func GetDB() *gorm.DB {
	return db
}

func InitDB(config *config.Config) error {
	// use a tempDB variable so the shared variable is only set if there are no errors
	// shouldn't really matter a ton since the app should shut down if there are errors
	// but this doesn't hurt
	tempDB, err := gorm.Open(sqlite.Open(config.Libp2p.DbPath), &gorm.Config{})
	if err != nil {
		return err
	}
	// add persistence classes here
	if err := tempDB.AutoMigrate(&Connection{}); err != nil {
		return err
	}
	db = tempDB
	return nil
}

type connectionStatus int

const (
	requestingReceiver = iota
	requestingWitness
	open
	closed
)

type Connection struct {
	Id       uuid.UUID `gorm:"primaryKey"`
	Sender   string
	Witness  string
	Receiver string
	Status   connectionStatus
}

func CreateOutgoingConnection(targetNode string) (*Connection, error) {
	connection := Connection{uuid.New(), p2p.GetFullAddr(), targetNode, "", requestingReceiver}
	tx := db.Create(connection)
	if tx.Error != nil {
		return nil, tx.Error
	}
	return &connection, nil
}
