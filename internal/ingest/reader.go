package ingest

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// File is the materialised content of a ServiceNow xlsx export. Headers is
// the first row (cell values normalised); Rows contains every subsequent
// row padded or truncated to len(Headers). Numeric cells (ServiceNow's
// date columns are OLE serials per the Phase 1 pre-flight inspection)
// stay as raw strings — the mapper converts them via
// excelize.ExcelDateToTime so we never rely on locale-dependent display
// formatting from the styled number-format code.
type File struct {
	Headers []string
	Rows    [][]string
}

// Read loads an xlsx file fully into memory and returns the header + data
// rows. Phase 1 Risk #2 acknowledges excelize's memory cost is trivial at
// the Phase 0 sample size (412 rows / 249 KB); if future exports reach
// GB-scale, the plan schedules a switch to a streaming reader.
//
// RawCellValue: true is passed to rows.Columns so numeric date cells come
// through as OLE serial strings (e.g. "45123.418...") rather than
// workbook-formatted strings. The mapper owns the conversion to time.Time
// so a locale-sensitive display format can't silently reshape the value.
func Read(path string) (*File, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("ingest: open xlsx %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	if sheet == "" {
		return nil, fmt.Errorf("ingest: xlsx %s has no sheets", path)
	}

	rowIter, err := f.Rows(sheet)
	if err != nil {
		return nil, fmt.Errorf("ingest: iterate rows in %s: %w", path, err)
	}
	defer func() { _ = rowIter.Close() }()

	opts := excelize.Options{RawCellValue: true}
	var headers []string
	var data [][]string
	rowNum := 0

	for rowIter.Next() {
		rowNum++
		cells, err := rowIter.Columns(opts)
		if err != nil {
			return nil, fmt.Errorf("ingest: read row %d of %s: %w", rowNum, path, err)
		}
		if rowNum == 1 {
			headers = make([]string, len(cells))
			for i, c := range cells {
				headers[i] = NormaliseCell(c)
			}
			continue
		}
		padded := make([]string, len(headers))
		for i := range headers {
			if i < len(cells) {
				padded[i] = NormaliseCell(cells[i])
			}
		}
		data = append(data, padded)
	}
	if err := rowIter.Error(); err != nil {
		return nil, fmt.Errorf("ingest: row iterator on %s: %w", path, err)
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("ingest: xlsx %s has no header row", path)
	}
	return &File{Headers: headers, Rows: data}, nil
}
