# The site

Not built yet. There is nothing worth publishing until kuma answers a query.

When there is, this directory holds a static site generated from `results/`. Not a dashboard, not a service, and nothing that needs a server: a few pages of tables and line charts, regenerated after every nightly run and served from GitHub Pages.

## What it will show

The table everyone comes for, meaning kuma against pandas and Polars for every query at every size, with the losses in it.

A line per query over time, which is the part the table cannot show and the reason the results are committed. A table says which library is faster today. A line says which week it got slower and what landed that week.

The machine each number came from, next to the number rather than in a footnote. Results from a shared runner and results from a dedicated machine are not the same kind of evidence and should not look alike.

Every query's source, one click away, in all three libraries. A benchmark where you cannot read what was actually run is a claim rather than a measurement.

## What it will not show

A single headline number. There is no such thing as a dataframe engine's speed, and every project that has published one has been embarrassed by it later.
