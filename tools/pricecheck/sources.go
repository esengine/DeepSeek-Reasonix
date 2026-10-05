// sources.go — where each vendor publishes its rates, and how to read them off.
package main

import (
	"fmt"
	"reasonix/internal/contract/pricing"
	"strconv"
	"strings"
)

// published is one rate a vendor's page states for a model.
type published struct {
	Model string
	Rate  pricing.RateCard
}

// source is one vendor's price page in one currency. Read is nil where no
// reader exists and Unread then says why: a source nobody can read reports as
// unchecked, never as agreeing, because agreeing is a claim about the vendor.
type source struct {
	Provider string
	Currency string
	Models   []string
	URL      string
	Read     func(t table, models []string) ([]published, error)
	Unread   string
}

func sources() []source {
	return []source{
		{
			Provider: "deepseek", Currency: "USD",
			Models: []string{"deepseek-flash", "deepseek-v4-pro"},
			URL:    pricing.DocDeepSeekPricing,
			Read:   readDeepSeek,
		},
		{
			Provider: "longcat", Currency: "USD",
			Models: []string{"LongCat-2.0"},
			URL:    pricing.DocLongCatPricingUSD,
			Read:   readLongCat,
		},
		{
			Provider: "longcat", Currency: "CNY",
			Models: []string{"LongCat-2.0"},
			URL:    pricing.DocLongCatPricingCNY,
			Read:   readLongCat,
		},
		{
			Provider: "mimo", Currency: "CNY",
			Models: []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-flash"},
			URL:    pricing.DocMiMoPAYG,
			Unread: "the page renders its prices in the browser; fetching it returns no table",
		},
	}
}

// readDeepSeek reads the model columns off the pricing table. The wanted rates
// are the off-peak ones, which sit on the row that names them; the peak row
// below carries no label of its own and is never asked for.
func readDeepSeek(t table, models []string) ([]published, error) {
	// Matched on the names themselves: "deepseek-" alone also picks up the
	// model-version row, which spells the same models a different way.
	stated, ok := t.rowContaining(models...)
	if !ok {
		return nil, fmt.Errorf("no single row naming %v", models)
	}
	if named := modelNames(stated); !equalStrings(named, models) {
		return nil, fmt.Errorf("page prices %v, the local table expects %v", named, models)
	}
	cacheHit, err := amountsIn(t, models, "cache hit", "off-peak")
	if err != nil {
		return nil, err
	}
	input, err := amountsIn(t, models, "cache miss", "off-peak")
	if err != nil {
		return nil, err
	}
	output, err := amountsIn(t, models, "output tokens", "off-peak")
	if err != nil {
		return nil, err
	}
	out := make([]published, 0, len(models))
	for i, model := range models {
		out = append(out, published{Model: model, Rate: pricing.RateCard{
			CacheHit: cacheHit[i], Input: input[i], Output: output[i],
		}})
	}
	return out, nil
}

// readLongCat takes the last amount on each row, which is the discounted column
// the local table records. A page that stops discounting drops that column, and
// the last amount is then the list price — which is what should be compared.
func readLongCat(t table, models []string) ([]published, error) {
	if len(models) != 1 {
		return nil, fmt.Errorf("this page prices one model, the local table expects %d", len(models))
	}
	rate := pricing.RateCard{}
	// Labelled cells, in both languages the vendor publishes. A label it stops
	// using reads as unreadable, which is the point: the alternative is a
	// checker that quietly compares nothing and reports agreement.
	for _, field := range []struct {
		into   *float64
		labels []string
	}{
		{&rate.CacheHit, []string{"Cached Input", "输入（命中缓存）"}},
		{&rate.Input, []string{"Uncached Input", "输入（未命中缓存）"}},
		{&rate.Output, []string{"Output", "输出"}},
	} {
		row, ok := t.rowWithCell(field.labels...)
		if !ok {
			return nil, fmt.Errorf("no single row labelled %v", field.labels)
		}
		amounts := amountsOf(row)
		if len(amounts) == 0 {
			return nil, fmt.Errorf("row %v states no amount", field.labels)
		}
		*field.into = amounts[len(amounts)-1]
	}
	return []published{{Model: models[0], Rate: rate}}, nil
}

// amountsIn finds the row carrying every marker and requires one amount per
// model. A count that does not match means the page changed shape, which is
// reported rather than trimmed to fit.
func amountsIn(t table, models []string, markers ...string) ([]float64, error) {
	row, ok := t.rowContaining(markers...)
	if !ok {
		return nil, fmt.Errorf("no single row for %v", markers)
	}
	amounts := amountsOf(row)
	if len(amounts) != len(models) {
		return nil, fmt.Errorf("row %v states %d amounts for %d models", markers, len(amounts), len(models))
	}
	return amounts, nil
}

func modelNames(row []string) []string {
	var out []string
	for _, cell := range row {
		name, _, _ := strings.Cut(cell, "(")
		if name = strings.TrimSpace(name); strings.HasPrefix(name, "deepseek-") {
			out = append(out, name)
		}
	}
	return out
}

func amountsOf(row []string) []float64 {
	var out []float64
	for _, cell := range row {
		if amount, ok := parseAmount(cell); ok {
			out = append(out, amount)
		}
	}
	return out
}

// parseAmount accepts the symbols these pages price in. A cell carrying
// anything else is not an amount, which is how a label is told from a rate.
func parseAmount(cell string) (float64, bool) {
	cell = strings.TrimSpace(cell)
	for _, symbol := range []string{"$", "¥", "￥", "US$"} {
		cell = strings.TrimPrefix(cell, symbol)
	}
	cell = strings.TrimSuffix(strings.TrimSpace(cell), "元")
	amount, err := strconv.ParseFloat(strings.TrimSpace(cell), 64)
	return amount, err == nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
