package api

import "net/http"

// getStats serves GET /api/stats. Phase 2.1 shape is counts only; 2.2 will
// add priority-bucket counts + stale_count.
func (a *API) getStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r)
	defer cancel()

	stats, err := a.queries.Stats(ctx)
	if err != nil {
		writeError(w, a.logger, http.StatusInternalServerError, ErrInternal, "failed to load stats")
		a.logger.Error().Err(err).Msg("stats: query failed")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
