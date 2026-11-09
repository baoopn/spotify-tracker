-- +goose Up
-- +goose StatementBegin

-- search_fold folds text for accent- and case-insensitive search: NFD splits
-- accented letters into base letter + combining marks, the marks
-- (U+0300–U+036F) are removed, đ/Đ/ð/Ð (no decomposition) become d/D, and the
-- result is lowercased. "Ngày rời chuyến bay" -> "ngay roi chuyen bay".
-- LIKE wildcards and the backslash escape pass through unchanged. No
-- extension (unaccent) is needed. normalize() requires a UTF8 database.
CREATE OR REPLACE FUNCTION search_fold(text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS
$$ SELECT lower(translate(regexp_replace(normalize($1, NFD), '[\u0300-\u036f]', '', 'g'), 'đĐðÐ', 'dDdD')) $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS search_fold(text);
-- +goose StatementEnd
