package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/openpost/backend/internal/api/middleware"
	analyticsservice "github.com/openpost/backend/internal/services/analytics"
	"github.com/xuri/excelize/v2"
)

const (
	analyticsExportCSV  = "csv"
	analyticsExportXLSX = "xlsx"
	analyticsExportJSON = "json"
)

type ExportAnalyticsInput struct {
	WorkspaceID string `query:"workspace_id" required:"true" doc:"Workspace ID"`
	Days        int    `query:"days" default:"30" doc:"Reporting window: 7, 30, 90, 365 days, or 0 for all time. Custom dates override days."`
	From        string `query:"from" doc:"Inclusive start date (YYYY-MM-DD, UTC), supplied together with to"`
	To          string `query:"to" doc:"Inclusive end date (YYYY-MM-DD, UTC), supplied together with from"`
	AccountIDs  string `query:"account_ids" doc:"Optional comma-separated social account IDs. Intersects with account_id and platforms when supplied."`
	AccountID   string `query:"account_id" doc:"Optional social account ID to export"`
	Platforms   string `query:"platforms" doc:"Optional comma-separated platform filter, for example x,bluesky"`
	Format      string `query:"format" default:"csv" enum:"csv,xlsx,json" doc:"File format. xlsx contains every table plus charts."`
	Timezone    string `query:"timezone" default:"UTC" doc:"IANA timezone used for publish times and day, week and month grouping"`
	GroupBy     string `query:"group_by" default:"post" enum:"post,day,week,month,platform,account" doc:"Row grouping for the report"`
	Dataset     string `query:"dataset" default:"report" enum:"report,posts,daily,accounts" doc:"Table to export for csv and json. report follows group_by; xlsx always includes all tables."`
	Metrics     string `query:"metrics" doc:"Optional comma-separated post metric columns for posts and grouped reports. Defaults to every measured metric. Daily and accounts tables retain their fixed columns."`
}

type ExportAnalyticsOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

func (h *AnalyticsHandler) registerExportRoute(api huma.API) {
	binary := &huma.MediaType{Schema: &huma.Schema{Type: "string", Format: "binary"}}
	huma.Register(api, huma.Operation{
		OperationID: "export-analytics",
		Method:      http.MethodGet,
		Path:        "/analytics/export",
		Summary:     "Export stored analytics as CSV, Excel, or JSON",
		Description: "Exports the same stored measurements shown in Analytics. Missing provider metrics stay empty instead of becoming zero.",
		Tags:        []string{tagAnalytics},
		Middlewares: huma.Middlewares{middleware.AuthMiddleware(api, h.auth)},
		Errors:      []int{400, 403, 500},
		Responses: map[string]*huma.Response{
			"200": {Description: "Analytics export file", Content: map[string]*huma.MediaType{
				"text/csv":         binary,
				"application/json": binary,
				"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": binary,
			}},
		},
	}, func(ctx context.Context, input *ExportAnalyticsInput) (*ExportAnalyticsOutput, error) {
		if err := h.requireWorkspaceAccess(ctx, input.WorkspaceID, false); err != nil {
			return nil, err
		}
		if err := analyticsservice.ValidateRange(input.Days, input.From, input.To); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		location, err := loadExportLocation(input.Timezone)
		if err != nil {
			return nil, huma.Error400BadRequest("timezone must be an IANA timezone such as Europe/Rome")
		}
		dataset, err := h.service.BuildExport(ctx, input.WorkspaceID, analyticsservice.ExportOptions{
			From: input.From, To: input.To, Days: input.Days,
			AccountID:  input.AccountID,
			AccountIDs: splitList(input.AccountIDs),
			Platforms:  splitList(input.Platforms),
			GroupBy:    input.GroupBy,
			Metrics:    splitList(input.Metrics),
			Location:   location,
		})
		if errors.Is(err, analyticsservice.ErrInvalidRange) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if errors.Is(err, analyticsservice.ErrInvalidExportOptions) {
			return nil, huma.Error400BadRequest("invalid analytics export options")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to build analytics export")
		}

		name := fmt.Sprintf("openpost-analytics-%s-%s_%s", dataset.GroupBy, dataset.From, dataset.To)
		var (
			body        []byte
			contentType string
		)
		switch input.Format {
		case analyticsExportXLSX:
			body, err = analyticsExportWorkbook(dataset)
			contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		case analyticsExportJSON:
			body, err = json.MarshalIndent(analyticsExportSelection(dataset, input.Dataset), "", "  ")
			contentType = "application/json"
		default:
			input.Format = analyticsExportCSV
			body, err = analyticsExportCSVBytes(analyticsExportSelection(dataset, input.Dataset))
			contentType = "text/csv; charset=utf-8"
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to encode analytics export")
		}
		return &ExportAnalyticsOutput{
			ContentType:        contentType,
			ContentDisposition: fmt.Sprintf("attachment; filename=%q", name+"."+input.Format),
			Body:               body,
		}, nil
	})
}

func loadExportLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "utc") {
		return time.UTC, nil
	}
	if strings.EqualFold(name, "local") {
		return nil, errors.New("local timezone is not portable")
	}
	return time.LoadLocation(name)
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func analyticsExportSelection(dataset analyticsservice.ExportDataset, name string) analyticsservice.ExportTable {
	switch name {
	case "posts":
		return dataset.Posts
	case "daily":
		return dataset.Daily
	case "accounts":
		return dataset.Accounts
	default:
		return dataset.Grouped
	}
}

// analyticsExportCSVBytes writes a UTF-8 BOM so Excel detects the encoding.
// Text cells that a spreadsheet could read as a formula are prefixed with an
// apostrophe, because post titles are user-controlled.
func analyticsExportCSVBytes(table analyticsservice.ExportTable) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(table.Columns); err != nil {
		return nil, err
	}
	for _, row := range table.Rows {
		record := make([]string, len(row))
		for index, cell := range row {
			switch value := cell.(type) {
			case nil:
			case string:
				record[index] = neutralizeSpreadsheetFormula(value)
			default:
				record[index] = fmt.Sprint(value)
			}
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}

// analyticsExportHandle matches plain account handles such as @name. Without
// parentheses or spaces they cannot form a formula, and an apostrophe would
// corrupt a value people filter and join on.
var analyticsExportHandle = regexp.MustCompile(`^@[\w.\-]+$`)

func neutralizeSpreadsheetFormula(value string) string {
	if analyticsExportHandle.MatchString(value) {
		return value
	}
	first, _ := utf8.DecodeRuneInString(value)
	if strings.ContainsRune("=+-@\t\r\n＝＋－＠", first) {
		return "'" + value
	}
	return value
}

var analyticsExportGroupSheet = map[string]string{
	analyticsservice.ExportGroupDay:      "ByDay",
	analyticsservice.ExportGroupWeek:     "ByWeek",
	analyticsservice.ExportGroupMonth:    "ByMonth",
	analyticsservice.ExportGroupPlatform: "ByPlatform",
	analyticsservice.ExportGroupAccount:  "ByAccount",
}

func analyticsExportWorkbook(dataset analyticsservice.ExportDataset) ([]byte, error) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()

	header, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F2937"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return nil, err
	}

	summary := "Summary"
	if err := file.SetSheetName("Sheet1", summary); err != nil {
		return nil, err
	}
	rows := [][]any{
		{"SocialFlow.ai analytics export"},
		{"Generated", dataset.GeneratedAt.Format("2006-01-02 15:04")},
		{"Timezone", dataset.Timezone},
		{"Period (UTC days)", dataset.From + " to " + dataset.To},
		{"Range (days)", dataset.RangeDays},
		{"Grouped by", dataset.GroupBy},
		{"Posts exported", len(dataset.Posts.Rows)},
		{},
		{"Notes"},
		{"Empty cells mean a missing measurement or a metric that cannot be aggregated. They are not zero."},
		{"Publish times and day, week and month groups use the selected timezone."},
		{"Daily trends use UTC calendar days, as the Analytics charts do."},
		{"Grouped reports sum lifetime counters; averages and reporting-period measurements remain in Posts."},
		{"Post values retain stored integer units, including milliseconds and basis points."},
	}
	for index, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, index+1)
		if err := file.SetSheetRow(summary, cell, &row); err != nil {
			return nil, err
		}
	}
	_ = file.SetColWidth(summary, "A", "A", 24)
	_ = file.SetColWidth(summary, "B", "B", 40)

	type sheet struct {
		name  string
		table analyticsservice.ExportTable
	}
	sheets := []sheet{}
	if dataset.GroupBy != analyticsservice.ExportGroupPost {
		sheets = append(sheets, sheet{analyticsExportGroupSheet[dataset.GroupBy], dataset.Grouped})
	}
	sheets = append(sheets, sheet{"Posts", dataset.Posts}, sheet{"Daily", dataset.Daily}, sheet{"Accounts", dataset.Accounts})

	for _, current := range sheets {
		if _, err := file.NewSheet(current.name); err != nil {
			return nil, err
		}
		if err := writeExportSheet(file, current.name, current.table, header); err != nil {
			return nil, err
		}
	}

	if dataset.GroupBy != analyticsservice.ExportGroupPost {
		if err := addGroupedChart(file, analyticsExportGroupSheet[dataset.GroupBy], dataset); err != nil {
			return nil, err
		}
	}
	if err := addDailyChart(file, dataset.Daily); err != nil {
		return nil, err
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeExportSheet(file *excelize.File, name string, table analyticsservice.ExportTable, header int) error {
	headerRow := make([]any, len(table.Columns))
	for index, column := range table.Columns {
		headerRow[index] = column
	}
	if err := file.SetSheetRow(name, "A1", &headerRow); err != nil {
		return err
	}
	for rowIndex, row := range table.Rows {
		cell, _ := excelize.CoordinatesToCellName(1, rowIndex+2)
		if err := file.SetSheetRow(name, cell, &row); err != nil {
			return err
		}
	}
	last, _ := excelize.CoordinatesToCellName(len(table.Columns), 1)
	if err := file.SetCellStyle(name, "A1", last, header); err != nil {
		return err
	}
	if err := file.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	if len(table.Rows) > 0 {
		end, _ := excelize.CoordinatesToCellName(len(table.Columns), len(table.Rows)+1)
		if err := file.AutoFilter(name, "A1:"+end, nil); err != nil {
			return err
		}
	}
	lastColumn, _ := excelize.ColumnNumberToName(len(table.Columns))
	return file.SetColWidth(name, "A", lastColumn, 16)
}

func addGroupedChart(file *excelize.File, sheet string, dataset analyticsservice.ExportDataset) error {
	rows := len(dataset.Grouped.Rows)
	if rows == 0 {
		return nil
	}
	chartType := excelize.Col
	if dataset.GroupBy == analyticsservice.ExportGroupDay ||
		dataset.GroupBy == analyticsservice.ExportGroupWeek ||
		dataset.GroupBy == analyticsservice.ExportGroupMonth {
		chartType = excelize.Line
	}
	series := []excelize.ChartSeries{{
		Name:       sheet + "!$C$1",
		Categories: fmt.Sprintf("%s!$A$2:$A$%d", sheet, rows+1),
		Values:     fmt.Sprintf("%s!$C$2:$C$%d", sheet, rows+1),
	}}
	for index, column := range dataset.Grouped.Columns {
		if column == analyticsservice.ExportMetricLabel("views") {
			letter, _ := excelize.ColumnNumberToName(index + 1)
			series = append(series, excelize.ChartSeries{
				Name:       fmt.Sprintf("%s!$%s$1", sheet, letter),
				Categories: fmt.Sprintf("%s!$A$2:$A$%d", sheet, rows+1),
				Values:     fmt.Sprintf("%s!$%s$2:$%s$%d", sheet, letter, letter, rows+1),
			})
		}
	}
	anchor, _ := excelize.CoordinatesToCellName(len(dataset.Grouped.Columns)+2, 2)
	return file.AddChart(sheet, anchor, &excelize.Chart{
		Type:      chartType,
		Series:    series,
		Title:     excelize.ChartTitle{Paragraph: []excelize.RichTextRun{{Text: "Performance by " + dataset.GroupBy}}},
		Legend:    excelize.ChartLegend{Position: "bottom"},
		Dimension: excelize.ChartDimension{Width: 720, Height: 360},
	})
}

func addDailyChart(file *excelize.File, daily analyticsservice.ExportTable) error {
	rows := len(daily.Rows)
	if rows == 0 {
		return nil
	}
	series := []excelize.ChartSeries{}
	for _, column := range []string{"C", "D"} {
		series = append(series, excelize.ChartSeries{
			Name:       fmt.Sprintf("Daily!$%s$1", column),
			Categories: fmt.Sprintf("Daily!$A$2:$A$%d", rows+1),
			Values:     fmt.Sprintf("Daily!$%s$2:$%s$%d", column, column, rows+1),
		})
	}
	return file.AddChart("Daily", "G2", &excelize.Chart{
		Type:      excelize.Line,
		Series:    series,
		Title:     excelize.ChartTitle{Paragraph: []excelize.RichTextRun{{Text: "Engagement and views per day"}}},
		Legend:    excelize.ChartLegend{Position: "bottom"},
		Dimension: excelize.ChartDimension{Width: 720, Height: 360},
	})
}
