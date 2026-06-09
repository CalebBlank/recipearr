package tandoor

import "encoding/json"

// ---- Create payload (POST /api/recipe/) ----
// Confirmed against live Tandoor: only Name + Steps are required. food/unit/keyword
// auto-create by name. Unit must be PRESENT on each ingredient (object or null).

type RecipeCreate struct {
	Name         string        `json:"name"`
	Description   string        `json:"description"`
	Internal     bool          `json:"internal"`
	WorkingTime  int           `json:"working_time"`
	WaitingTime  int           `json:"waiting_time"`
	Servings     int           `json:"servings"`
	ServingsText string        `json:"servings_text"`
	SourceURL    string        `json:"source_url"`
	Keywords     []KeywordRef  `json:"keywords"`
	Steps        []StepCreate  `json:"steps"`
}

type KeywordRef struct {
	Name string `json:"name"`
}

type StepCreate struct {
	Instruction          string             `json:"instruction"`
	Ingredients          []IngredientCreate `json:"ingredients"`
	ShowIngredientsTable bool               `json:"show_ingredients_table"`
	Order                int                `json:"order"`
}

type IngredientCreate struct {
	Food         FoodRef  `json:"food"`
	Unit         *UnitRef `json:"unit"` // nil -> serialized as null (unit-less ingredient)
	Amount       float64  `json:"amount"`
	Note         string   `json:"note"`
	OriginalText string   `json:"original_text"`
	NoAmount     bool     `json:"no_amount"`
	Order        int      `json:"order"`
}

type FoodRef struct {
	Name string `json:"name"`
}

type UnitRef struct {
	Name string `json:"name"`
}

// ---- recipe-from-source response (POST /api/recipe-from-source/) ----

type RFSResponse struct {
	Recipe     *RFSRecipe     `json:"recipe"`
	RecipeID   *int           `json:"recipe_id"`
	Images     []string       `json:"images"`
	Error      bool           `json:"error"`
	Msg        string         `json:"msg"`
	Duplicates []RFSDuplicate `json:"duplicates"`
}

type RFSRecipe struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Steps        []RFSStep         `json:"steps"`
	Keywords     []RFSKeyword      `json:"keywords"`
	WorkingTime  int               `json:"working_time"`
	WaitingTime  int               `json:"waiting_time"`
	Servings     int               `json:"servings"`
	ServingsText string            `json:"servings_text"`
	SourceURL    string            `json:"source_url"`
	ImageURL     string            `json:"image_url"`
	Internal     bool              `json:"internal"`
	Properties   []json.RawMessage `json:"properties"`
}

type RFSStep struct {
	Instruction          string          `json:"instruction"`
	Ingredients          []RFSIngredient `json:"ingredients"`
	ShowIngredientsTable bool            `json:"show_ingredients_table"`
}

type RFSIngredient struct {
	Amount       float64  `json:"amount"`
	Unit         *RFSUnit `json:"unit"`
	Food         *RFSFood `json:"food"`
	Note         string   `json:"note"`
	OriginalText string   `json:"original_text"`
}

type RFSUnit struct {
	Name string `json:"name"`
}

type RFSFood struct {
	Name string `json:"name"`
}

type RFSKeyword struct {
	ID            *int   `json:"id"`
	Name          string `json:"name"`
	Label         string `json:"label"`
	ImportKeyword bool   `json:"import_keyword"`
}

type RFSDuplicate struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ---- Recipe books (GET/POST /api/recipe-book/, POST /api/recipe-book-entry/) ----

type Book struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type bookListResponse struct {
	Count   int    `json:"count"`
	Next    string `json:"next"`
	Results []Book `json:"results"`
}

// bookCreate: Tandoor requires `shared` to be present (empty list is fine).
type bookCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Shared      []int  `json:"shared"`
}

type bookEntryCreate struct {
	Book   int `json:"book"`
	Recipe int `json:"recipe"`
}

// ---- Food list (GET /api/food/) ----

type Food struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	PluralName string `json:"plural_name"`
}

type foodListResponse struct {
	Count   int    `json:"count"`
	Next    string `json:"next"`
	Results []Food `json:"results"`
}

// createResponse is the subset of the created recipe we care about.
type createResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
