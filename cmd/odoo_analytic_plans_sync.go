package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	odoosource "github.com/CommonsHub/chb/providers/odoo"
)

// OdooAnalyticPlansFile is the local cache produced by the
// "analytic plans" stage of `chb odoo sync`. It records the plan and
// account ids the categorize command needs to set the right
// analytic_distribution on each line — without these, categorize would
// need to hit Odoo for every line.
type OdooAnalyticPlansFile struct {
	SchemaVersion int                     `json:"schemaVersion"`
	FetchedAt     string                  `json:"fetchedAt"`
	Plans         OdooAnalyticPlanIDs     `json:"plans"`
	Categories    []OdooAnalyticAccountID `json:"categories"`
	Collectives   []OdooAnalyticAccountID `json:"collectives"`
}

type OdooAnalyticPlanIDs struct {
	Collective int `json:"collective"` // plan id 3 by convention
	Costs      int `json:"costs"`      // plan id 8 by convention
	Income     int `json:"income"`     // created if missing
}

type OdooAnalyticAccountID struct {
	Slug      string `json:"slug"`      // category or collective slug
	Name      string `json:"name"`      // display name
	PlanID    int    `json:"planId"`    // which plan the account lives on
	AccountID int    `json:"accountId"` // account.analytic.account id
}

const odooAnalyticPlansSchemaVersion = 1

// syncOdooAnalyticInfrastructure ensures every plan + analytic.account
// referenced by the categorize step exists in Odoo. Idempotent: re-runs
// only create what's missing. Returns the resulting cache for callers
// that want to act on it immediately (categorize).
func syncOdooAnalyticInfrastructure(creds *OdooCredentials, uid int) (*OdooAnalyticPlansFile, error) {
	plans, err := ensureOdooAnalyticPlans(creds, uid)
	if err != nil {
		return nil, fmt.Errorf("plans: %v", err)
	}

	// Existing accounts indexed by (plan_id, lowercased name) so we can
	// reuse instead of creating duplicates.
	existing, err := fetchOdooAnalyticAccountsByPlan(creds, uid, []int{plans.Collective, plans.Costs, plans.Income})
	if err != nil {
		return nil, fmt.Errorf("accounts: %v", err)
	}

	// Categories: each odoo_rule with a non-empty category becomes an
	// analytic account on the costs or income plan, depending on the
	// rule's direction.
	wantCategories, err := categoryAccountSpecs(plans)
	if err != nil {
		return nil, fmt.Errorf("category specs: %v", err)
	}
	catAccounts, err := ensureOdooAnalyticAccounts(creds, uid, wantCategories, existing)
	if err != nil {
		return nil, fmt.Errorf("category accounts: %v", err)
	}

	// Collectives: every unique collective slug referenced in rules.json
	// becomes an analytic account on plan 3.
	wantCollectives, err := collectiveAccountSpecs(plans)
	if err != nil {
		return nil, fmt.Errorf("collective specs: %v", err)
	}
	collAccounts, err := ensureOdooAnalyticAccounts(creds, uid, wantCollectives, existing)
	if err != nil {
		return nil, fmt.Errorf("collective accounts: %v", err)
	}

	file := &OdooAnalyticPlansFile{
		SchemaVersion: odooAnalyticPlansSchemaVersion,
		FetchedAt:     time.Now().UTC().Format(time.RFC3339),
		Plans:         plans,
		Categories:    catAccounts,
		Collectives:   collAccounts,
	}
	if err := saveOdooAnalyticPlansFile(file); err != nil {
		return nil, fmt.Errorf("save cache: %v", err)
	}
	return file, nil
}

// OdooAnalyticPlansSync is the `chb odoo sync` step entry. Mirrors the
// shape of OdooPartnersSync so it slots into OdooSyncAll cleanly.
func OdooAnalyticPlansSync(args []string) (int, error) {
	if HasFlag(args, "--help", "-h", "help") {
		printOdooSyncHelp()
		return 0, nil
	}
	creds, err := ResolveOdooCredentials()
	if err != nil {
		return 0, err
	}
	uid, err := odooAuth(creds.URL, creds.DB, creds.Login, creds.Password)
	if err != nil {
		return 0, err
	}
	if uid == 0 {
		return 0, fmt.Errorf("Odoo authentication failed")
	}
	odooLog("\n%s📊 Syncing Odoo analytic plans%s\n", Fmt.Bold, Fmt.Reset)
	file, err := syncOdooAnalyticInfrastructure(creds, uid)
	if err != nil {
		return 0, err
	}
	total := len(file.Categories) + len(file.Collectives)
	odooSyncLine("analytic plans", odooItemSyncStatus(total, "analytic account", ""))
	return total, nil
}

// ensureOdooAnalyticPlans returns the plan ids for collective/costs/income,
// creating the income plan if missing. Collective and costs use the
// well-known ids 3 and 8 by convention; we look them up by id and surface
// an error if they don't exist (the operator needs to create them in
// Odoo Studio first since they're part of the chart-of-accounts setup).
func ensureOdooAnalyticPlans(creds *OdooCredentials, uid int) (OdooAnalyticPlanIDs, error) {
	const (
		collectivePlanID = 3
		costsPlanID      = 8
	)

	rows, err := odooSearchReadAllMaps(creds, uid, "account.analytic.plan",
		[]interface{}{},
		[]string{"id", "name"},
		"id asc")
	if err != nil {
		return OdooAnalyticPlanIDs{}, err
	}

	have := map[int]string{}
	incomeID := 0
	for _, row := range rows {
		id := odooInt(row["id"])
		name := odooString(row["name"])
		have[id] = name
		if isIncomePlanName(name) && incomeID == 0 {
			incomeID = id
		}
	}

	plans := OdooAnalyticPlanIDs{}
	if _, ok := have[collectivePlanID]; ok {
		plans.Collective = collectivePlanID
	} else {
		return plans, fmt.Errorf("analytic plan #%d (collective) not found — create it in Odoo first", collectivePlanID)
	}
	if _, ok := have[costsPlanID]; ok {
		plans.Costs = costsPlanID
	} else {
		return plans, fmt.Errorf("analytic plan #%d (costs) not found — create it in Odoo first", costsPlanID)
	}
	if incomeID > 0 {
		plans.Income = incomeID
	} else {
		id, err := createOdooAnalyticPlan(creds, uid, "Income")
		if err != nil {
			return plans, fmt.Errorf("create income plan: %v", err)
		}
		plans.Income = id
	}
	return plans, nil
}

func isIncomePlanName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "income" || n == "revenue" || n == "incomes"
}

func createOdooAnalyticPlan(creds *OdooCredentials, uid int, name string) (int, error) {
	data, err := odooExec(creds.URL, creds.DB, uid, creds.Password,
		"account.analytic.plan", "create",
		[]interface{}{[]interface{}{map[string]interface{}{"name": name}}}, nil)
	if err != nil {
		return 0, err
	}
	ids := parseOdooCreatedIDs(data)
	if len(ids) == 0 {
		return 0, fmt.Errorf("no id returned")
	}
	return ids[0], nil
}

// analyticAccountSpec describes one analytic account we want to exist.
// It is consumed by ensureOdooAnalyticAccounts which idempotently creates
// missing rows.
type analyticAccountSpec struct {
	Slug   string
	Name   string
	PlanID int
}

// categoryAccountSpecs walks the OdooMapping chain. Each mapping with a
// non-empty category produces one spec on the income plan (direction:in)
// or the costs plan (direction:out). internal_transfer is excluded —
// it has no analytic account by design (account 580001 is enough).
func categoryAccountSpecs(plans OdooAnalyticPlanIDs) ([]analyticAccountSpec, error) {
	mappings, err := LoadOdooMappings()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]analyticAccountSpec, 0, len(mappings))
	for _, r := range mappings {
		cat := strings.TrimSpace(r.Match.Category)
		if cat == "" {
			continue
		}
		if cat == "internal_transfer" {
			continue
		}
		key := strings.ToLower(cat)
		if seen[key] {
			continue
		}
		seen[key] = true
		planID := plans.Costs
		switch strings.ToLower(strings.TrimSpace(r.Match.Direction)) {
		case "in":
			planID = plans.Income
		case "out", "":
			planID = plans.Costs
		}
		out = append(out, analyticAccountSpec{
			Slug:   key,
			Name:   prettyCategoryName(cat),
			PlanID: planID,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// collectiveAccountSpecs gathers every distinct collective slug from
// rules.json so each gets one analytic account on the collective plan.
func collectiveAccountSpecs(plans OdooAnalyticPlanIDs) ([]analyticAccountSpec, error) {
	rules, err := LoadRules()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]analyticAccountSpec, 0, len(rules))
	for _, r := range rules {
		coll := strings.TrimSpace(r.Assign.Collective)
		if coll == "" {
			continue
		}
		key := strings.ToLower(coll)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, analyticAccountSpec{
			Slug:   key,
			Name:   prettyCollectiveName(coll),
			PlanID: plans.Collective,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func prettyCategoryName(slug string) string {
	parts := strings.Split(slug, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func prettyCollectiveName(slug string) string {
	return prettyCategoryName(slug)
}

// fetchOdooAnalyticAccountsByPlan returns a map keyed by
// (planID, lowercased name) → accountID for accounts on the given plans.
// Used by ensureOdooAnalyticAccounts to avoid duplicate creates.
func fetchOdooAnalyticAccountsByPlan(creds *OdooCredentials, uid int, planIDs []int) (map[string]int, error) {
	out := map[string]int{}
	planArg := make([]interface{}, 0, len(planIDs))
	for _, p := range planIDs {
		if p > 0 {
			planArg = append(planArg, p)
		}
	}
	if len(planArg) == 0 {
		return out, nil
	}
	rows, err := odooSearchReadAllMaps(creds, uid, "account.analytic.account",
		[]interface{}{
			[]interface{}{"plan_id", "in", planArg},
			[]interface{}{"active", "=", true},
		},
		[]string{"id", "name", "plan_id"},
		"id asc",
	)
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		planID := odooFieldID(row["plan_id"])
		name := strings.ToLower(strings.TrimSpace(odooString(row["name"])))
		if planID > 0 && name != "" {
			out[analyticAccountKey(planID, name)] = odooInt(row["id"])
		}
	}
	return out, nil
}

func analyticAccountKey(planID int, name string) string {
	return fmt.Sprintf("%d:%s", planID, strings.ToLower(name))
}

// ensureOdooAnalyticAccounts creates any missing accounts and returns
// the resulting cache entries. existing is mutated in-place so a single
// fetch can be reused across category + collective passes.
func ensureOdooAnalyticAccounts(creds *OdooCredentials, uid int, specs []analyticAccountSpec, existing map[string]int) ([]OdooAnalyticAccountID, error) {
	out := make([]OdooAnalyticAccountID, 0, len(specs))
	for _, spec := range specs {
		key := analyticAccountKey(spec.PlanID, spec.Name)
		if id, ok := existing[key]; ok && id > 0 {
			out = append(out, OdooAnalyticAccountID{
				Slug:      spec.Slug,
				Name:      spec.Name,
				PlanID:    spec.PlanID,
				AccountID: id,
			})
			continue
		}
		id, err := createOdooAnalyticAccount(creds, uid, spec.Name, spec.PlanID)
		if err != nil {
			return out, fmt.Errorf("create %s (plan %d): %v", spec.Name, spec.PlanID, err)
		}
		existing[key] = id
		out = append(out, OdooAnalyticAccountID{
			Slug:      spec.Slug,
			Name:      spec.Name,
			PlanID:    spec.PlanID,
			AccountID: id,
		})
	}
	return out, nil
}

func createOdooAnalyticAccount(creds *OdooCredentials, uid int, name string, planID int) (int, error) {
	data, err := odooExec(creds.URL, creds.DB, uid, creds.Password,
		"account.analytic.account", "create",
		[]interface{}{[]interface{}{map[string]interface{}{
			"name":    name,
			"plan_id": planID,
		}}}, nil)
	if err != nil {
		return 0, err
	}
	ids := parseOdooCreatedIDs(data)
	if len(ids) == 0 {
		return 0, fmt.Errorf("no id returned")
	}
	return ids[0], nil
}

func odooAnalyticPlansCachePath() string {
	return odoosource.Path(DataDir(), "latest", "", odoosource.AnalyticPlansFile)
}

func saveOdooAnalyticPlansFile(file *OdooAnalyticPlansFile) error {
	return odoosource.WriteJSON(DataDir(), "latest", "", file, odoosource.AnalyticPlansFile)
}

// loadOdooAnalyticPlansFile reads the cache written by the analytic plans
// sync. Callers (categorize) use the AccountID lookups to set
// analytic_distribution on move lines. Returns nil when the cache is
// missing — the caller is expected to suggest `chb odoo sync`.
func loadOdooAnalyticPlansFile() *OdooAnalyticPlansFile {
	data, err := os.ReadFile(odooAnalyticPlansCachePath())
	if err != nil {
		return nil
	}
	var file OdooAnalyticPlansFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil
	}
	return &file
}

// CategoryAccountIDFor looks up the analytic account id for a category
// slug. Returns 0 if not found.
func (f *OdooAnalyticPlansFile) CategoryAccountIDFor(slug string) int {
	if f == nil {
		return 0
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	for _, a := range f.Categories {
		if a.Slug == slug {
			return a.AccountID
		}
	}
	return 0
}

// CollectiveAccountIDFor looks up the analytic account id for a
// collective slug. Returns 0 if not found.
func (f *OdooAnalyticPlansFile) CollectiveAccountIDFor(slug string) int {
	if f == nil {
		return 0
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	for _, a := range f.Collectives {
		if a.Slug == slug {
			return a.AccountID
		}
	}
	return 0
}
