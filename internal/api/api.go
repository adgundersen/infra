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
	"github.com/adgundersen/crimata-infra/internal/dns"
	"github.com/adgundersen/crimata-infra/internal/export"
	"github.com/adgundersen/crimata-infra/internal/instance"
	"github.com/adgundersen/crimata-infra/internal/notify"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store   *instance.Store
	compute *compute.Client
	dns     *dns.Client
	notify  *notify.Client
	export  *export.Client
}

func NewHandler(
	store *instance.Store,
	compute *compute.Client,
	dns *dns.Client,
	notify *notify.Client,
	export *export.Client,
) *Handler {
	return &Handler{store: store, compute: compute, dns: dns, notify: notify, export: export}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Post("/instances", h.createInstance)
	r.Get("/instances/{slug}", h.getInstance)
	r.Delete("/instances/{slug}", h.deleteInstance)
	return r
}

type createRequest struct {
	StripeCustomerID     string `json:"stripe_customer_id"`
	StripeSubscriptionID string `json:"stripe_subscription_id"`
	Email                string `json:"email"`
}

func (h *Handler) createInstance(w http.ResponseWriter, r *http.Request) {
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

	inst := &instance.Instance{
		StripeCustomerID: req.StripeCustomerID,
		Slug:             slug,
		Status:           instance.StatusProvisioning,
	}
	if err := h.store.Create(inst); err != nil {
		http.Error(w, "failed to create instance record", http.StatusInternalServerError)
		return
	}

	go h.provision(context.Background(), inst, req.Email, password, dbPassword)

	jsonResponse(w, inst, http.StatusAccepted)
}

func (h *Handler) getInstance(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	inst, err := h.store.GetBySlug(slug)
	if err != nil || inst == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, inst, http.StatusOK)
}

func (h *Handler) deleteInstance(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	inst, err := h.store.GetBySlug(slug)
	if err != nil || inst == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	go h.deprovision(context.Background(), inst)
	w.WriteHeader(http.StatusAccepted)
}

// ── Provisioning ──────────────────────────────────────────────────────────────

func (h *Handler) provision(ctx context.Context, inst *instance.Instance, email, password, dbPassword string) {
	// 1. Launch EC2
	ec2, err := h.compute.Launch(ctx, inst.Slug)
	if err != nil {
		fmt.Printf("provision: launch failed for %s: %v\n", inst.Slug, err)
		h.store.UpdateStatus(inst.ID, instance.StatusFailed)
		return
	}
	h.store.UpdateEC2(inst.ID, ec2.InstanceID, ec2.PublicIP)
	h.store.UpdateSSHKey(inst.ID, ec2.SSHPrivateKey)

	// 2. Wait for instance to be ready
	if err := h.compute.WaitUntilReady(ctx, ec2.InstanceID, ec2.PublicIP); err != nil {
		fmt.Printf("provision: wait failed for %s: %v\n", inst.Slug, err)
		h.store.UpdateStatus(inst.ID, instance.StatusFailed)
		return
	}

	// 3. Run provisioning script via SSH
	if err := h.compute.Provision(ctx, ec2.PublicIP, ec2.SSHPrivateKey, inst.Slug, password, dbPassword); err != nil {
		fmt.Printf("provision: script failed for %s: %v\n", inst.Slug, err)
		h.store.UpdateStatus(inst.ID, instance.StatusFailed)
		return
	}

	// 4. Create Route53 record
	if err := h.dns.CreateRecord(ctx, inst.Slug, ec2.PublicIP); err != nil {
		fmt.Printf("provision: dns failed for %s: %v\n", inst.Slug, err)
	}

	// 5. Send welcome email
	if err := h.notify.SendWelcome(ctx, email, inst.Slug, password); err != nil {
		fmt.Printf("provision: email failed for %s: %v\n", inst.Slug, err)
	}

	h.store.UpdateStatus(inst.ID, instance.StatusActive)
	fmt.Printf("provision: %s is live at %s.crimata.com\n", email, inst.Slug)
}

func (h *Handler) deprovision(ctx context.Context, inst *instance.Instance) {
	h.store.UpdateStatus(inst.ID, instance.StatusCancelled)

	// 1. Export data and email download link
	downloadURL, err := h.export.Export(ctx, inst.EC2InstanceID, inst.Slug)
	if err != nil {
		fmt.Printf("deprovision: export failed for %s: %v\n", inst.Slug, err)
	} else {
		h.notify.SendDataExport(ctx, inst.StripeCustomerID, downloadURL)
	}

	// 2. Terminate EC2
	if err := h.compute.Terminate(ctx, inst.EC2InstanceID); err != nil {
		fmt.Printf("deprovision: terminate failed for %s: %v\n", inst.Slug, err)
	}

	// 3. Remove Route53 record
	if err := h.dns.DeleteRecord(ctx, inst.Slug, inst.EC2PublicIP); err != nil {
		fmt.Printf("deprovision: dns delete failed for %s: %v\n", inst.Slug, err)
	}

	fmt.Printf("deprovision: %s cleaned up\n", inst.Slug)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func uniqueSlug(store *instance.Store, email string) string {
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
