package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/adgundersen/crimata-infra/internal/compute"
	"github.com/adgundersen/crimata-infra/internal/customer"
	"github.com/adgundersen/crimata-infra/internal/dns"
	"github.com/adgundersen/crimata-infra/internal/export"
	"github.com/adgundersen/crimata-infra/internal/notify"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store   *customer.Store
	compute *compute.Client
	dns     *dns.Client
	notify  *notify.Client
	export  *export.Client
}

func NewHandler(
	store *customer.Store,
	compute *compute.Client,
	dns *dns.Client,
	notify *notify.Client,
	export *export.Client,
) *Handler {
	return &Handler{store: store, compute: compute, dns: dns, notify: notify, export: export}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/customers", h.createCustomer)
	r.Get("/customers/{slug}", h.getCustomer)
	r.Delete("/customers/{slug}", h.deleteCustomer)
	return r
}

type createRequest struct {
	StripeCustomerID     string `json:"stripe_customer_id"`
	StripeSubscriptionID string `json:"stripe_subscription_id"`
	Email                string `json:"email"`
}

func (h *Handler) createCustomer(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Idempotency
	existing, _ := h.store.GetByStripeID(req.StripeCustomerID)
	if existing != nil {
		jsonResponse(w, existing, http.StatusOK)
		return
	}

	slug       := uniqueSlug(h.store, req.Email)
	password   := randomHex(12)
	dbPassword := randomHex(24)

	c := &customer.Customer{
		StripeCustomerID:     req.StripeCustomerID,
		StripeSubscriptionID: req.StripeSubscriptionID,
		Email:                req.Email,
		Slug:                 slug,
		Status:               customer.StatusProvisioning,
	}
	if err := h.store.Create(c); err != nil {
		http.Error(w, "failed to create customer record", http.StatusInternalServerError)
		return
	}

	go h.provision(context.Background(), c, password, dbPassword)

	w.WriteHeader(http.StatusAccepted)
	jsonResponse(w, c, http.StatusAccepted)
}

func (h *Handler) getCustomer(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := h.store.GetBySlug(slug)
	if err != nil || c == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, c, http.StatusOK)
}

func (h *Handler) deleteCustomer(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	c, err := h.store.GetBySlug(slug)
	if err != nil || c == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	go h.deprovision(context.Background(), c)
	w.WriteHeader(http.StatusAccepted)
}

// ── Provisioning ──────────────────────────────────────────────────────────────

func (h *Handler) provision(ctx context.Context, c *customer.Customer, password, dbPassword string) {
	// 1. Launch EC2
	instance, err := h.compute.Launch(ctx, c.Slug)
	if err != nil {
		fmt.Printf("provision: launch failed for %s: %v\n", c.Email, err)
		h.store.UpdateStatus(c.ID, customer.StatusFailed)
		return
	}
	h.store.UpdateEC2(c.ID, instance.InstanceID, instance.PublicIP)

	// 2. Wait for instance to be ready
	if err := h.compute.WaitUntilReady(ctx, instance.InstanceID); err != nil {
		fmt.Printf("provision: wait failed for %s: %v\n", c.Email, err)
		h.store.UpdateStatus(c.ID, customer.StatusFailed)
		return
	}

	// 3. Run provisioning script via SSM
	if err := h.compute.Provision(ctx, instance.InstanceID, c.Slug, password, dbPassword); err != nil {
		fmt.Printf("provision: script failed for %s: %v\n", c.Email, err)
		h.store.UpdateStatus(c.ID, customer.StatusFailed)
		return
	}

	// 4. Create Route53 record
	if err := h.dns.CreateRecord(ctx, c.Slug, instance.PublicIP); err != nil {
		fmt.Printf("provision: dns failed for %s: %v\n", c.Email, err)
	}

	// 5. Send welcome email
	if err := h.notify.SendWelcome(ctx, c.Email, c.Slug, password); err != nil {
		fmt.Printf("provision: email failed for %s: %v\n", c.Email, err)
	}

	h.store.UpdateStatus(c.ID, customer.StatusActive)
	fmt.Printf("provision: %s is live at %s.crimata.com\n", c.Email, c.Slug)
}

func (h *Handler) deprovision(ctx context.Context, c *customer.Customer) {
	h.store.UpdateStatus(c.ID, customer.StatusCancelled)

	// 1. Export data and email download link
	downloadURL, err := h.export.Export(ctx, c.EC2InstanceID, c.Slug)
	if err != nil {
		fmt.Printf("deprovision: export failed for %s: %v\n", c.Email, err)
	} else {
		h.notify.SendDataExport(ctx, c.Email, downloadURL)
	}

	// 2. Terminate EC2
	if err := h.compute.Terminate(ctx, c.EC2InstanceID); err != nil {
		fmt.Printf("deprovision: terminate failed for %s: %v\n", c.Email, err)
	}

	// 3. Remove Route53 record
	if err := h.dns.DeleteRecord(ctx, c.Slug, c.EC2PublicIP); err != nil {
		fmt.Printf("deprovision: dns delete failed for %s: %v\n", c.Email, err)
	}

	fmt.Printf("deprovision: %s cleaned up\n", c.Email)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func uniqueSlug(store *customer.Store, email string) string {
	base := strings.Trim(slugRe.ReplaceAllString(
		strings.ToLower(strings.Split(email, "@")[0]), "-"), "-")
	if len(base) > 20 {
		base = base[:20]
	}
	slug := base
	for i := 2; ; i++ {
		existing, _ := store.GetBySlug(slug)
		if existing == nil {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func jsonResponse(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
