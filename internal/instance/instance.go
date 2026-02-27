package instance

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

type Instance struct {
	ID               int64     `json:"id"`
	StripeCustomerID string    `json:"stripe_customer_id"`
	Slug             string    `json:"slug"`
	EC2InstanceID    string    `json:"ec2_instance_id"`
	EC2PublicIP      string    `json:"ec2_public_ip"`
	SSHPrivateKey    string    `json:"-"`
	Status           Status    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS instances (
			id                  SERIAL PRIMARY KEY,
			stripe_customer_id  TEXT UNIQUE NOT NULL,
			slug                TEXT UNIQUE NOT NULL,
			ec2_instance_id     TEXT NOT NULL DEFAULT '',
			ec2_public_ip       TEXT NOT NULL DEFAULT '',
			ssh_private_key     TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'provisioning',
			created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func (s *Store) Create(inst *Instance) error {
	return s.db.QueryRow(`
		INSERT INTO instances (stripe_customer_id, slug, status)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`,
		inst.StripeCustomerID, inst.Slug, inst.Status,
	).Scan(&inst.ID, &inst.CreatedAt)
}

func (s *Store) GetByStripeID(stripeCustomerID string) (*Instance, error) {
	inst := &Instance{}
	err := s.db.QueryRow(`
		SELECT id, stripe_customer_id, slug, ec2_instance_id, ec2_public_ip,
		       ssh_private_key, status, created_at
		FROM instances WHERE stripe_customer_id = $1`,
		stripeCustomerID,
	).Scan(
		&inst.ID, &inst.StripeCustomerID, &inst.Slug,
		&inst.EC2InstanceID, &inst.EC2PublicIP,
		&inst.SSHPrivateKey, &inst.Status, &inst.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return inst, err
}

func (s *Store) GetBySlug(slug string) (*Instance, error) {
	inst := &Instance{}
	err := s.db.QueryRow(`
		SELECT id, stripe_customer_id, slug, ec2_instance_id, ec2_public_ip,
		       ssh_private_key, status, created_at
		FROM instances WHERE slug = $1`,
		slug,
	).Scan(
		&inst.ID, &inst.StripeCustomerID, &inst.Slug,
		&inst.EC2InstanceID, &inst.EC2PublicIP,
		&inst.SSHPrivateKey, &inst.Status, &inst.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return inst, err
}

func (s *Store) UpdateSSHKey(id int64, privateKey string) error {
	_, err := s.db.Exec(`UPDATE instances SET ssh_private_key = $1 WHERE id = $2`, privateKey, id)
	return err
}

func (s *Store) UpdateEC2(id int64, instanceID, publicIP string) error {
	_, err := s.db.Exec(
		`UPDATE instances SET ec2_instance_id = $1, ec2_public_ip = $2 WHERE id = $3`,
		instanceID, publicIP, id,
	)
	return err
}

func (s *Store) UpdateStatus(id int64, status Status) error {
	_, err := s.db.Exec(`UPDATE instances SET status = $1 WHERE id = $2`, status, id)
	return err
}
