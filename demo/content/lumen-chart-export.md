# Chart export to PNG and CSV

## Goal

Every chart widget gets an export menu offering **PNG** and **CSV**.

## Proposed changes

- **[NEW]** `src/charts/export.ts`: `toPNG(chart, { scale: 2 })` from the chart's canvas, and `toCSV(series)` with one column per series
- **[MODIFY]** `ChartCard.tsx`: overflow menu with "Download PNG" and "Download CSV"
- File names follow `<dashboard>-<chart>-<yyyy-mm-dd>.<ext>`
- PNGs always use the light theme, so exports look the same in slides

## Verification plan

- Vitest for `toCSV` with gaps and misaligned timestamps
- Playwright: export both formats from the overview dashboard and check the downloads
