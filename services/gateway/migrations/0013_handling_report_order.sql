CREATE INDEX disclosure_handling_reports_recent
 ON disclosure_handling_reports(receipt_id,recorded_at DESC,report_id DESC);
