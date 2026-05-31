package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type providersListResponse struct {
	Providers []ProviderResponse `json:"providers"`
}

type providerSingleResponse struct {
	Provider ProviderResponse `json:"provider"`
}

func (c *Connector) registerProviderRoutes() {
	if c == nil || c.providerRoutesRegistered || c.Bridge == nil {
		return
	}
	matrix, ok := c.Bridge.Matrix.(bridgev2.MatrixConnectorWithProvisioning)
	if !ok {
		return
	}
	provisioning := matrix.GetProvisioning()
	if provisioning == nil || provisioning.GetRouter() == nil {
		return
	}
	router := provisioning.GetRouter()
	router.HandleFunc("GET /v3/providers", c.handleProvidersList(provisioning))
	router.HandleFunc("POST /v3/providers", c.handleProvidersCreate(provisioning))
	router.HandleFunc("GET /v3/providers/{providerID}", c.handleProvidersGet(provisioning))
	router.HandleFunc("PUT /v3/providers/{providerID}", c.handleProvidersUpdate(provisioning))
	router.HandleFunc("DELETE /v3/providers/{providerID}", c.handleProvidersDelete(provisioning))
	c.providerRoutesRegistered = true
}

func (c *Connector) handleProvidersList(provisioning bridgev2.IProvisioningAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		login, err := c.loginForProviderRequest(r.Context(), provisioning.GetUser(r), r.URL.Query().Get("login_id"))
		if err != nil {
			writeProviderError(w, providerErrorStatus(err), err)
			return
		}
		writeProviderJSON(w, http.StatusOK, providersListResponse{Providers: sortedProviderResponses(c.providersForLogin(login))})
	}
}

func (c *Connector) handleProvidersGet(provisioning bridgev2.IProvisioningAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		login, err := c.loginForProviderRequest(r.Context(), provisioning.GetUser(r), r.URL.Query().Get("login_id"))
		if err != nil {
			writeProviderError(w, providerErrorStatus(err), err)
			return
		}
		providerID := strings.TrimSpace(r.PathValue("providerID"))
		provider, ok := c.providerForLogin(login, providerID)
		if !ok {
			mautrix.MNotFound.WithMessage("Provider not found").Write(w)
			return
		}
		writeProviderJSON(w, http.StatusOK, providerSingleResponse{Provider: providerResponse(provider)})
	}
}

func (c *Connector) handleProvidersCreate(provisioning bridgev2.IProvisioningAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.handleProviderUpsert(w, r, provisioning, "")
	}
}

func (c *Connector) handleProvidersUpdate(provisioning bridgev2.IProvisioningAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.handleProviderUpsert(w, r, provisioning, strings.TrimSpace(r.PathValue("providerID")))
	}
}

func (c *Connector) handleProviderUpsert(w http.ResponseWriter, r *http.Request, provisioning bridgev2.IProvisioningAPI, routeProviderID string) {
	var input ProviderInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		mautrix.MBadJSON.WithMessage("Invalid provider request").Write(w)
		return
	}
	if routeProviderID != "" {
		if input.ID != "" && input.ID != routeProviderID {
			mautrix.MBadJSON.WithMessage("Provider ID does not match route").Write(w)
			return
		}
		input.ID = routeProviderID
	}
	provider, err := c.VerifyProviderConfig(r.Context(), input)
	if err != nil {
		writeProviderError(w, http.StatusBadRequest, err)
		return
	}
	login, err := c.loginForProviderRequest(r.Context(), provisioning.GetUser(r), r.URL.Query().Get("login_id"))
	if err != nil {
		writeProviderError(w, providerErrorStatus(err), err)
		return
	}
	if err = c.SaveProviderConfig(r.Context(), login, provider); err != nil {
		writeProviderError(w, providerErrorStatus(err), err)
		return
	}
	status := http.StatusCreated
	if routeProviderID != "" {
		status = http.StatusOK
	}
	writeProviderJSON(w, status, providerSingleResponse{Provider: providerResponse(provider)})
}

func (c *Connector) handleProvidersDelete(provisioning bridgev2.IProvisioningAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		login, err := c.loginForProviderRequest(r.Context(), provisioning.GetUser(r), r.URL.Query().Get("login_id"))
		if err != nil {
			writeProviderError(w, providerErrorStatus(err), err)
			return
		}
		err = c.DeleteProvider(r.Context(), login, r.PathValue("providerID"))
		if err != nil {
			if strings.Contains(err.Error(), "managed by Beeper AI") {
				mautrix.MForbidden.WithMessage(err.Error()).Write(w)
			} else if strings.Contains(err.Error(), "not found") {
				mautrix.MNotFound.WithMessage(err.Error()).Write(w)
			} else {
				writeProviderError(w, http.StatusInternalServerError, err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (c *Connector) loginForProviderRequest(ctx context.Context, user *bridgev2.User, rawLoginID string) (*bridgev2.UserLogin, error) {
	loginID := strings.TrimSpace(rawLoginID)
	if loginID == "" {
		return c.EnsureAIChatsLogin(ctx, user)
	}
	login, err := c.Bridge.GetExistingUserLoginByID(ctx, networkid.UserLoginID(loginID))
	if err != nil {
		return nil, err
	}
	if login == nil || login.UserMXID != user.MXID {
		return nil, fmt.Errorf("login %s not found", loginID)
	}
	if err := c.ensureAIChatsMetadata(ctx, login); err != nil {
		return nil, err
	}
	return login, nil
}

func writeProviderJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func providerErrorStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if strings.Contains(err.Error(), "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(err.Error(), "managed by Beeper AI") {
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func writeProviderError(w http.ResponseWriter, status int, err error) {
	if status >= 500 {
		mautrix.MUnknown.WithMessage(err.Error()).WithStatus(status).Write(w)
	} else {
		mautrix.MBadJSON.WithMessage(err.Error()).WithStatus(status).Write(w)
	}
}
