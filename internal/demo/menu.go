package demo

// MenuItem represents a drink on the Blend menu.
type MenuItem struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	BasePrice   float64 `json:"basePrice"`
}

// Size represents a drink size.
type Size struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Mult  float64 `json:"multiplier"`
}

// Sizes available for all drinks.
var Sizes = []Size{
	{ID: "v1", Label: "Small", Mult: 1.0},
	{ID: "v2", Label: "Medium", Mult: 1.3},
	{ID: "v3", Label: "Large", Mult: 1.6},
}

// Menu is the static list of drinks served at Blend.
var Menu = []MenuItem{
	{
		Name:        "The Interface",
		Description: "House blend drip coffee. Clean, reliable, always compatible.",
		Category:    "coffee",
		BasePrice:   3.50,
	},
	{
		Name:        "Schema Latte",
		Description: "Classic latte with structured layers of espresso and steamed milk.",
		Category:    "espresso",
		BasePrice:   4.75,
	},
	{
		Name:        "The Drift",
		Description: "Seasonal cold brew. Today's batch may differ from yesterday's.",
		Category:    "cold",
		BasePrice:   5.00,
	},
	{
		Name:        "Binding Brew",
		Description: "Double shot espresso. Connects you to the day ahead.",
		Category:    "espresso",
		BasePrice:   3.00,
	},
	{
		Name:        "Event Stream",
		Description: "Smooth matcha green tea. Continuous, calming, always flowing.",
		Category:    "tea",
		BasePrice:   4.25,
	},
}

// FindDrink returns the menu item with the given name, or nil.
func FindDrink(name string) *MenuItem {
	for i := range Menu {
		if Menu[i].Name == name {
			return &Menu[i]
		}
	}
	return nil
}

// FindSize returns the size with the given ID, or nil.
func FindSize(id string) *Size {
	for i := range Sizes {
		if Sizes[i].ID == id {
			return &Sizes[i]
		}
	}
	return nil
}
