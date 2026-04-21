package api

import "net/http"

// listClients serves GET /api/clients.
func (a *API) listClients(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r)
	defer cancel()

	items, err := a.queries.ListClients(ctx)
	if err != nil {
		writeError(w, a.logger, http.StatusInternalServerError, ErrInternal, "failed to list clients")
		a.logger.Error().Err(err).Msg("clients list: query failed")
		return
	}

	writeJSON(w, http.StatusOK, ClientListEnvelope{Data: items})
}
