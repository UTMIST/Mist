package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/UTMIST/Mist/internal/executor/k3s"
	"github.com/UTMIST/Mist/internal/identity"
)

// cleanup handles POST /internal/v1/executions/{execution_id}/cleanup: a
// node's cleanup reporter delivering the receipt from `mistd
// cleanup-report`. A host may only report for itself; the executor then
// checks the receipt against its own records before anything is released.
func (s *Server) cleanup(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	if s.Cleanup == nil {
		return unavailable("cleanup receipts are not accepted by this API server")
	}
	var rc k3s.CleanupReceipt
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rc); err != nil {
		return invalid("", "receipt: "+err.Error())
	}
	if rc.ExecutionID != r.PathValue("execution_id") {
		return invalid("execution_id", "receipt is for a different execution")
	}
	if rc.Node != p.Host {
		return &Error{http.StatusForbidden, CodeForbidden, "a host can only report cleanup on itself", nil}
	}
	if err := s.Cleanup.RecordCleanup(r.Context(), rc); errors.Is(err, k3s.ErrCleanupRejected) {
		return &Error{http.StatusConflict, CodeConflict, err.Error(), nil}
	} else if err != nil {
		return err
	}
	s.Log.Info("cleanup receipt recorded", "execution", rc.ExecutionID, "node", rc.Node)
	if err := s.Jobs.Wake(r.Context()); err != nil {
		s.Log.Warn("wake scheduler", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
	return nil
}
