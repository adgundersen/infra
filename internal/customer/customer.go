package customer

import (
	"database/sql"
	"time"
)

type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusActive       Status = "active"
	StatusFailed       Status = "failed"
	StatusCancelled    Status = "cancelled"
)

type Customer struct {
	ID                   int64
	StripeCustomerID     string
	StripeSubscriptionID string
	Email                string
	Slug                 string
	EC2InstanceID        string
	EC2PublicIP          string
	SSHPrivateKey        string
	Status               Status
	CreatedAt            time.Time
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS customers (
			id                     SERIAL PRIMARY KEY,
			stripe_customer_id     TEXT UNIQUE NOT NULL,
			stripe_subscription_id TEXT UNIQUE NOT NULL,
			email                  TEXT NOT NULL,
			slug                   TEXT UNIQUE NOT NULL,
			ec2_instance_id        TEXT NOT NULL DEFAULT '',
			ec2_public_ip          TEXT NOT NULL DEFAULT '',
			ssh_private_key        TEXT NOT NULL DEFAULT '',
			status                 TEXT NOT NULL DEFAULT 'provisioning',
			created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return err
	}
	// Add ssh_private_key column if it doesn't exist (migration for existing tables)
	_, _ = s.db.Exec(`ALTER TABLE customers ADD COLUMN IF NOT EXISTS ssh_private_key TEXT NOT NULL DEFAULT ''`)
	return nil
}

func (s *Store) Create(c *Customer) error {
	return s.db.QueryRow(`
		INSERT INTO customers
			(stripe_customer_id, stripe_subscription_id, email, slug, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		c.StripeCustomerID, c.StripeSubscriptionID,
		c.Email, c.Slug, c.Status,
	).Scan(&c.ID, &c.CreatedAt)
}

func (s *Store) GetByStripeID(stripeCustomerID string) (*Customer, error) {
	c := &Customer{}
	err := s.db.QueryRow(`
		SELECT id, stripe_customer_id, stripe_subscription_id, email, slug,
		       ec2_instance_id, ec2_public_ip, ssh_private_key, status, created_at
		FROM customers WHERE stripe_customer_id = $1`,
		stripeCustomerID,
	).Scan(
		&c.ID, &c.StripeCustomerID, &c.StripeSubscriptionID,
		&c.Email, &c.Slug, &c.EC2InstanceID, &c.EC2PublicIP,
		&c.SSHPrivateKey, &c.Status, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (s *Store) GetBySlug(slug string) (*Customer, error) {
	c := &Customer{}
	err := s.db.QueryRow(`
		SELECT id, stripe_customer_id, stripe_subscription_id, email, slug,
		       ec2_instance_id, ec2_public_ip, ssh_private_key, status, created_at
		FROM customers WHERE slug = $1`,
		slug,
	).Scan(
		&c.ID, &c.StripeCustomerID, &c.StripeSubscriptionID,
		&c.Email, &c.Slug, &c.EC2InstanceID, &c.EC2PublicIP,
		&c.SSHPrivateKey, &c.Status, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (s *Store) UpdateSSHKey(id int64, privateKey string) error {
	_, err := s.db.Exec(`UPDATE customers SET ssh_private_key = $1 WHERE id = $2`, privateKey, id)
	return err
}

func (s *Store) UpdateEC2(id int64, instanceID, publicIP string) error {
	_, err := s.db.Exec(
		`UPDATE customers SET ec2_instance_id = $1, ec2_public_ip = $2 WHERE id = $3`,
		instanceID, publicIP, id,
	)
	return err
}

func (s *Store) UpdateStatus(id int64, status Status) error {
	_, err := s.db.Exec(`UPDATE customers SET status = $1 WHERE id = $2`, status, id)
	return err
}
