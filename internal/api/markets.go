package api

import "net/http"

// listMarkets serves GET /api/markets. country_code is a *string in the
// output — NULL becomes JSON null, representing the internal-services
// bucket. The web client renders it as "Internal".
func (a *API) listMarkets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r)
	defer cancel()

	items, err := a.queries.ListMarkets(ctx)
	if err != nil {
		writeError(w, a.logger, http.StatusInternalServerError, ErrInternal, "failed to list markets")
		a.logger.Error().Err(err).Msg("markets list: query failed")
		return
	}

	writeJSON(w, http.StatusOK, MarketListEnvelope{Data: items})
}
