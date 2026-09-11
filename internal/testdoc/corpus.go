package testdoc

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Realistic, larger documents with known facts, for the e2e suite and the A/B
// harness: big enough that reading them costs real tokens, with answers that
// can be checked by string match. Generation is deterministic.

// Facts stated exactly once in ReportDOCX.
const (
	ReportTitle        = "Northwind Traders Annual Report 2025"
	ReportEMEARevenue  = "$48.2 million"
	ReportApprover     = "Dana Whitfield"
	ReportApprovalDate = "14 March 2025"
	ReportHeadcount    = "1,284"
)

var reportRegions = []string{"North America", "EMEA", "Latin America", "Asia Pacific"}

// ReportDOCX is a ~30k-character annual report: headings three levels deep,
// long prose, bulleted and numbered lists, a hyperlink and two tables.
func ReportDOCX() []byte {
	r := rand.New(rand.NewPCG(2025, 7))
	var body []string
	add := func(parts ...string) { body = append(body, parts...) }
	para := func(text string) { add(WPara("", WRun(text, ""))) }

	add(WPara("Title", WRun(ReportTitle, "")))
	para("This report summarizes the financial results, operations and people of Northwind Traders for the fiscal year that ended on 31 December 2025.")

	add(WPara("Heading1", WRun("Executive summary", "")))
	for range 4 {
		para(prose(r, "the company", 5))
	}
	add(WItem("1", 0, "Revenue grew in every region for the third year in a row."))
	add(WItem("1", 0, "Operating margin improved to 14.6% after the warehouse consolidation."))
	add(WItem("1", 1, "The Rotterdam and Lyon sites were merged in the second quarter."))
	add(WItem("1", 0, "Customer satisfaction reached its highest level since 2019."))

	add(WPara("Heading1", WRun("Financial overview", "")))
	para("Revenue by region, in millions of US dollars:")
	add(WTextTable([][]string{
		{"Region", "2024", "2025", "Change"},
		{"North America", "61.7", "66.9", "+8.4%"},
		{"EMEA", "44.9", "48.2", "+7.3%"},
		{"Latin America", "12.3", "14.0", "+13.8%"},
		{"Asia Pacific", "21.5", "23.1", "+7.4%"},
		{"Total", "140.4", "152.2", "+8.4%"},
	}))
	for range 3 {
		para(prose(r, "the finance team", 6))
	}

	add(WPara("Heading1", WRun("Regional performance", "")))
	for _, region := range reportRegions {
		add(WPara("Heading2", WRun(region, "")))
		if region == "EMEA" {
			para("EMEA revenue was " + ReportEMEARevenue + " in 2025, up from $44.9 million the year before, with Germany and the Nordics contributing most of the growth.")
		}
		for _, sub := range []string{"Sales", "Operations"} {
			add(WPara("Heading3", WRun(region+" "+strings.ToLower(sub), "")))
			for range 3 {
				para(prose(r, "the "+region+" "+strings.ToLower(sub)+" team", 5))
			}
		}
	}

	add(WPara("Heading1", WRun("People", "")))
	para("At year end Northwind employed " + ReportHeadcount + " people in 14 countries, 9% more than a year earlier.")
	for range 3 {
		para(prose(r, "the people team", 5))
	}

	add(WPara("Heading1", WRun("Risks", "")))
	for _, risk := range []string{"Currency swings between the euro and the US dollar.", "Rising freight costs on Asia–Europe routes.", "Dependence on two suppliers for refrigerated goods.", "Cyber attacks on the ordering platform."} {
		add(WItem("2", 0, risk))
	}
	para(prose(r, "the risk committee", 5))

	add(WPara("Heading1", WRun("Budget 2026", "")))
	add(WPara("", WRun("The 2026 budget of $161 million was approved by ", ""), WRun(ReportApprover, "<w:b/>"),
		WRun(", Chief Financial Officer, on "+ReportApprovalDate+". Details are on the ", ""),
		WLink("rId1", WRun("investor site", "")), WRun(".", "")))
	para(prose(r, "the board", 4))

	add(WPara("Heading1", WRun("Appendix: monthly revenue", "")))
	rows := [][]string{{"Month", "North America", "EMEA", "Latin America", "Asia Pacific"}}
	for _, m := range []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"} {
		row := []string{m}
		for _, total := range []float64{66.9, 48.2, 14.0, 23.1} {
			row = append(row, fmt.Sprintf("%.2f", total/12*(0.9+r.Float64()*0.2)))
		}
		rows = append(rows, row)
	}
	add(WTextTable(rows))
	return DOCX(strings.Join(body, ""), map[string]string{"rId1": "https://investors.northwind.example/2025"})
}

// prose returns n plausible report sentences about subject.
func prose(r *rand.Rand, subject string, n int) string {
	templates := []string{
		"Over the year %s grew order volume by %d%%, helped by %s and a stronger %s.",
		"In the %s quarter %s cut average delivery time to %d hours for %s customers.",
		"Gross margin for %s reached %d.%d%%, compared with a plan of %d%%.",
		"%s signed %d new wholesale accounts, most of them in %s.",
		"Costs at %s rose %d%% because of %s, partly offset by %s.",
		"Following the %s review, %s moved %d%% of shipments to %s.",
		"Customer churn at %s fell to %d.%d%% after the %s program launched.",
		"%s expects %s to remain the main driver of growth in %d.",
	}
	fill := [][]string{
		{"new product lines", "online ordering", "better stock planning", "longer supplier contracts", "seasonal demand", "price changes"},
		{"first", "second", "third", "fourth"},
		{"dairy", "frozen food", "beverages", "bakery", "fresh produce", "household goods"},
		{"regional", "enterprise", "independent", "online"},
		{"energy prices", "higher wages", "new regulation", "fuel costs"},
		{"rail freight", "the new hub", "local carriers", "electric vans"},
	}
	pick := func(i int) string { return fill[i][r.IntN(len(fill[i]))] }
	var out []string
	for range n {
		var s string
		switch t := r.IntN(len(templates)); t {
		case 0:
			s = fmt.Sprintf(templates[t], subject, 3+r.IntN(20), pick(0), pick(3)+" segment")
		case 1:
			s = fmt.Sprintf(templates[t], pick(1), subject, 18+r.IntN(40), pick(3))
		case 2:
			s = fmt.Sprintf(templates[t], pick(2), 20+r.IntN(20), r.IntN(10), 20+r.IntN(20))
		case 3:
			s = fmt.Sprintf(templates[t], subject, 5+r.IntN(60), pick(2))
		case 4:
			s = fmt.Sprintf(templates[t], subject, 2+r.IntN(9), pick(4), pick(0))
		case 5:
			s = fmt.Sprintf(templates[t], pick(1)+"-quarter", subject, 5+r.IntN(30), pick(5))
		case 6:
			s = fmt.Sprintf(templates[t], subject, 1+r.IntN(6), r.IntN(10), pick(0))
		default:
			s = fmt.Sprintf(templates[t], subject, pick(2), 2026)
		}
		out = append(out, strings.ToUpper(s[:1])+s[1:])
	}
	return strings.Join(out, " ")
}

// Item is one row of InventoryXLSX.
type Item struct {
	SKU, Product, Category, Warehouse, Supplier string
	Stock                                       int
	UnitPrice                                   string
}

var (
	invAdjectives = []string{"Organic", "Classic", "Premium", "Family", "Mini", "Spicy", "Smoked", "Light"}
	invProducts   = []string{"Oat Milk", "Cheddar", "Espresso Beans", "Sourdough", "Green Tea", "Salsa", "Salmon", "Granola", "Olive Oil", "Honey"}
	invCategories = []string{"Dairy", "Bakery", "Beverages", "Pantry", "Frozen", "Seafood"}
	invWarehouses = []string{"Rotterdam", "Chicago", "São Paulo", "Singapore", "Dallas"}
	invSuppliers  = []string{"Fjord Foods", "Blue Ridge Farms", "Casa Verde", "Pacific Harvest", "Alpine Dairy", "Golden Grain Co."}
)

// InventoryItem is row i (1-based) of the inventory sheet.
func InventoryItem(i int) Item {
	return Item{
		SKU:       fmt.Sprintf("SKU-%04d", i),
		Product:   invAdjectives[(i*7)%len(invAdjectives)] + " " + invProducts[(i*3)%len(invProducts)],
		Category:  invCategories[(i*5)%len(invCategories)],
		Warehouse: invWarehouses[(i*11)%len(invWarehouses)],
		Supplier:  invSuppliers[(i*13)%len(invSuppliers)],
		Stock:     (i*37)%900 + 12,
		UnitPrice: fmt.Sprintf("%d.%02d", 2+(i*17)%48, (i*29)%100),
	}
}

// InventoryXLSX is a stock list with n items plus a small hidden sheet.
func InventoryXLSX(n int) []byte {
	rows := [][]any{{"SKU", "Product", "Category", "Warehouse", "Stock", "Unit price (USD)", "Supplier"}}
	for i := 1; i <= n; i++ {
		it := InventoryItem(i)
		rows = append(rows, []any{it.SKU, it.Product, it.Category, it.Warehouse, it.Stock, it.UnitPrice, it.Supplier})
	}
	return XLSX([]Sheet{
		{Name: "Inventory", Rows: rows},
		{Name: "Warehouses", Rows: [][]any{{"Warehouse", "Country"}, {"Rotterdam", "Netherlands"}, {"Chicago", "USA"}, {"São Paulo", "Brazil"}, {"Singapore", "Singapore"}, {"Dallas", "USA"}}},
		{Name: ".Notes", Rows: [][]any{{"internal draft"}}},
	})
}
