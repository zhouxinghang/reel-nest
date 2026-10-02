package http

import (
	"net/http"
	"strconv"
)

type actorCandidateDTO struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases"`
	Provider string   `json:"provider"`
	PhotoID  string   `json:"photoId"`
}

type actorDetailDTO struct {
	actorCandidateDTO
	Summary      string   `json:"summary"`
	Homepage     string   `json:"homepage"`
	Birthday     string   `json:"birthday"`
	DebutDate    string   `json:"debutDate"`
	Nationality  string   `json:"nationality"`
	Height       string   `json:"height"`
	BloodType    string   `json:"bloodType"`
	Hobby        string   `json:"hobby"`
	Skill        string   `json:"skill"`
	Measurements string   `json:"measurements"`
	CupSize      string   `json:"cupSize"`
	Wallpapers   []string `json:"wallpapers"`
}

func (t *Transport) actorSearch(w http.ResponseWriter, r *http.Request) {
	items, truncated, err := t.metadata.SearchActors(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		t.fail(w, err)
		return
	}
	result := struct {
		Items     []actorCandidateDTO `json:"items"`
		Truncated bool                `json:"truncated"`
	}{Items: []actorCandidateDTO{}, Truncated: truncated}
	for _, a := range items {
		result.Items = append(result.Items, actorCandidateDTO{a.ID, a.Name, a.Aliases, a.Ref.Namespace, a.PhotoID})
	}
	respond(w, 200, result)
}

func (t *Transport) actorDetail(w http.ResponseWriter, r *http.Request) {
	a, err := t.metadata.Actor(r.Context(), r.PathValue("id"))
	if err != nil {
		t.fail(w, err)
		return
	}
	respond(w, 200, actorDetailDTO{
		actorCandidateDTO: actorCandidateDTO{a.ID, a.Name, a.Aliases, a.Ref.Namespace, a.PhotoID},
		Summary:           a.Summary, Homepage: a.Homepage, Birthday: a.Birthday, DebutDate: a.DebutDate,
		Nationality: a.Nationality, Height: a.Height, BloodType: a.BloodType, Hobby: a.Hobby,
		Skill: a.Skill, Measurements: a.Measurements, CupSize: a.CupSize, Wallpapers: a.Wallpapers,
	})
}

func (t *Transport) actorImage(w http.ResponseWriter, r *http.Request) {
	body, mime, err := t.metadata.ActorImage(r.Context(), r.PathValue("id"))
	if err != nil {
		t.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(200)
	if r.Method != "HEAD" {
		_, _ = w.Write(body)
	}
}
