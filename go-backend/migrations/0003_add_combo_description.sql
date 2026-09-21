-- Carries over the Node app's "UNVERIFIED — ..." tagging convention for
-- auto-guessed combos (see catalog.AutoGuessFromSheet), so low-confidence
-- combo-component guesses stay visibly flagged for manual review.
ALTER TABLE combos ADD COLUMN IF NOT EXISTS description TEXT;
