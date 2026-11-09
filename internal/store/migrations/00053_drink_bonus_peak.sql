ALTER TABLE drink_bonus_effect ADD COLUMN peak REAL NOT NULL DEFAULT 0 CHECK(peak>=0 AND peak<=1);
