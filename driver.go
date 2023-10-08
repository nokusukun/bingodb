package bingo

import (
	"fmt"
	"github.com/go-playground/validator/v10"
	jsoniter "github.com/json-iterator/go"
	"go.etcd.io/bbolt"
	"os"
	"strings"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary
var AllDocuments = -1

type HasMarshal interface {
	Marshal(v interface{}) ([]byte, error)
}

type HasUnmarshal interface {
	Unmarshal(data []byte, v interface{}) error
}

var Marshaller HasMarshal = json
var Unmarshaller HasUnmarshal = json

var (
	ErrDocumentNotFound = fmt.Errorf("document not found")
	ErrDocumentExists   = fmt.Errorf("document already exists")
)

type WrappedBucket struct {
	*bbolt.Bucket
}

func (b *WrappedBucket) ReverseIter(fn func(k, v []byte) error) error {
	if b.Tx().DB() == nil {
		return fmt.Errorf("tx is closed")
	}
	c := b.Cursor()
	for k, v := c.Last(); k != nil; k, v = c.Prev() {
		if err := fn(k, v); err != nil {
			return err
		}
	}
	return nil
}

// IsErrDocumentNotFound returns true if the error is an ErrDocumentNotFound error.
func IsErrDocumentNotFound(err error) bool {
	return strings.Contains(err.Error(), ErrDocumentNotFound.Error())
}

// IsErrDocumentExists returns true if the error is an ErrDocumentExists error.
func IsErrDocumentExists(err error) bool {
	return strings.Contains(err.Error(), ErrDocumentExists.Error())
}

// DriverConfiguration represents the configuration for a database driver.
// DeleteNoVerify specifies whether to verify a Collection DROP operation before executing it.
// Filename specifies the filename of the database file.
type DriverConfiguration struct {
	DeleteNoVerify bool
	Filename       string
}

// Driver represents a database driver that manages collections of documents.
type Driver struct {
	db     *bbolt.DB
	val    *validator.Validate
	config *DriverConfiguration
	Closed bool
}

// NewDriver creates a new database driver with the specified configuration.
func NewDriver(config DriverConfiguration) (*Driver, error) {
	db, err := bbolt.Open(config.Filename, 0600, nil)
	if err != nil {
		return nil, err
	}
	return &Driver{
		db:     db,
		val:    validator.New(validator.WithRequiredStructEnabled()),
		config: &config,
	}, nil
}

// Close closes the database file.
func (d *Driver) Close() error {
	d.Closed = true
	return d.db.Close()
}

// Drop drops the collection from the database.
// If the environment variable BINGO_ALLOW_DROP_<COLLECTION_NAME> is not set to true, an error is returned.
// If Driver.config.DeleteNoVerify is set to true, the collection is dropped without any verification.
func (c *Collection[DocumentType]) Drop() error {
	if !c.Driver.config.DeleteNoVerify {
		if r, _ := os.LookupEnv("BINGO_ALLOW_DROP_" + strings.ToUpper(c.Name)); r != "true" {
			return fmt.Errorf("delete not allowed, set environment variable BINGO_ALLOW_DROP_%s=true to allow", strings.ToUpper(c.Name))
		}
	}
	_ = c.Driver.removeCollection(c.Name)
	return c.Driver.db.Update(func(tx *bbolt.Tx) error {
		return tx.DeleteBucket([]byte(c.Name))
	})
}

type Metadata struct {
	K string
	V any
}

func (m Metadata) Key() []byte {
	return []byte(m.K)
}

func (d *Driver) WriteMetadata(k string, v any) error {
	metadata := CollectionFrom[Metadata](d, "__metadata")
	_, err := metadata.Insert(Metadata{K: k, V: v}, Upsert)
	return err
}

func (d *Driver) ReadMetadata(k string) (any, error) {
	metadata := CollectionFrom[Metadata](d, "__metadata")
	r, err := metadata.FindOne(func(doc Metadata) bool {
		return doc.K == k
	})
	if err != nil {
		return nil, err
	}
	return r.V, nil
}

func (d *Driver) addCollection(name string) error {
	return d.WriteMetadata(fmt.Sprintf("collection:%v", name), true)
}

func (d *Driver) removeCollection(name string) error {
	return d.WriteMetadata(fmt.Sprintf("collection:%v", name), false)
}

func (d *Driver) GetCollections() ([]string, error) {
	metadata := CollectionFrom[Metadata](d, "__metadata")
	result, err := metadata.Find(func(doc Metadata) bool {
		return strings.HasPrefix(doc.K, "collection:") && doc.V.(bool)
	})
	if err != nil {
		return nil, err
	}
	var collections []string
	for _, doc := range result {
		collections = append(collections, strings.TrimPrefix(doc.K, "collection:"))
	}
	return collections, nil
}
