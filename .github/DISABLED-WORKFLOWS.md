# Disabled CI workflows

The six upstream OpenPost pipeline definitions were moved here from
`.github/workflows/` because they target upstream release/publishing
infrastructure (npm/n8n publishing, Pages headers, upstream secrets) and
cannot run against this independent fork. Content is preserved verbatim;
restore by moving the files back into `.github/workflows/`.
