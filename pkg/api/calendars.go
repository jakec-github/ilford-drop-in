package api

import (
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

func (h *Handler) handleCalendar(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	volunteerID, ok := strings.CutSuffix(filename, ".ics")
	if !ok || volunteerID == "" {
		h.writeError(w, http.StatusNotFound, "calendar not found")
		return
	}

	roles, err := services.RoleTable(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	volunteers, err := h.volunteers.ListVolunteers(h.cfg, roles)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	volunteer := findVolunteerByID(volunteers, volunteerID)
	if volunteer == nil {
		// The roster is whatever the last sync loaded; there is no self-fetch to
		// fall back on. A volunteer added to the sheet but not yet synced 404s
		// until an admin syncs — acceptable, since the editor is the syncer.
		h.writeError(w, http.StatusNotFound, "volunteer not found")
		return
	}

	shifts, err := services.ListShifts(r.Context(), h.store, h.volunteers, h.cfg, services.ListShiftsParams{}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	// When the drop-in runs is a setting an admin keeps, so a change to it
	// reaches every subscriber on their next poll rather than at the next
	// deploy. Settings nobody has filled in yield all-day events rather than a
	// failure — a subscription is not a feature to gate on them.
	defaults, err := services.RotaDefaults(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	calendar, err := services.BuildVolunteerCalendar(
		services.FilterShiftsByVolunteer(shifts, volunteerID),
		*volunteer,
		roles,
		defaults,
	)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	if _, err := w.Write([]byte(calendar)); err != nil {
		h.logger.Error("Failed to write calendar response", zap.Error(err))
	}
}

func findVolunteerByID(volunteers []model.Volunteer, id string) *model.Volunteer {
	for i := range volunteers {
		if volunteers[i].ID == id {
			return &volunteers[i]
		}
	}
	return nil
}
