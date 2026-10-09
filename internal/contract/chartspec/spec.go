package chartspec

type Spec struct {
	SpecVersion int    `json:"spec_version"`
	Title       string `json:"title"`
	Data        Data   `json:"data"`
	Marks       []Mark `json:"marks"`
	XAxis       *Axis  `json:"x_axis,omitempty"`
	YAxis       *Axis  `json:"y_axis,omitempty"`
}

type Data struct {
	Columns []Column `json:"columns"`
	Rows    [][]Cell `json:"rows"`
}

type ColumnType string

const (
	TypeNumber ColumnType = "number"
	TypeString ColumnType = "string"
	TypeDate   ColumnType = "date"
)

type Column struct {
	Name string     `json:"name"`
	Type ColumnType `json:"type"`
}

type MarkType string

const (
	MarkBar  MarkType = "bar"
	MarkLine MarkType = "line"
	MarkPie  MarkType = "pie"
)

// Mark draws columns of Data. X names the category or time column, Y the numeric
// columns plotted against it, Color an optional string column that splits every
// Y into one series per distinct value. Pie takes exactly one Y and no Color.
type Mark struct {
	Type    MarkType `json:"type"`
	X       string   `json:"x"`
	Y       []string `json:"y"`
	Color   string   `json:"color,omitempty"`
	Stacked bool     `json:"stacked,omitempty"`
	Donut   bool     `json:"donut,omitempty"`
}

type Scale string

const (
	ScaleLinear Scale = "linear"
	ScaleLog    Scale = "log"
)

// NumberFormat is a closed enum: the spec never carries a format string.
type NumberFormat string

const (
	FormatInteger NumberFormat = "integer"
	FormatDecimal NumberFormat = "decimal"
	FormatPercent NumberFormat = "percent"
)

type Axis struct {
	Title  string       `json:"title,omitempty"`
	Unit   string       `json:"unit,omitempty"`
	Scale  Scale        `json:"scale,omitempty"`
	Format NumberFormat `json:"format,omitempty"`
}
