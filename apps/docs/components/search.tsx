"use client";
import { useDocsSearch } from "fumadocs-core/search/client";
import { staticClient } from "fumadocs-core/search/client/orama-static";
import { useEffect, useRef, useState } from "react";
import { documentationSections } from "@/lib/sections";
import { docsPath } from "@openpost/social-images";
import { captureTelemetryEvent } from "@openpost/telemetry";
import {
  SearchDialog,
  SearchDialogContent,
  SearchDialogHeader,
  SearchDialogInput,
  SearchDialogList,
  SearchDialogOverlay,
  SearchDialogClose,
  TagsList,
  TagsListItem,
  type SharedProps,
} from "fumadocs-ui/components/dialog/search";

export default function Search(props: SharedProps) {
  const [tag, setTag] = useState<string>();
  const { search, setSearch, query } = useDocsSearch({
    client: staticClient({ tag, from: docsPath("/api/search") }),
  });
  const lastMeasured = useRef<string | null>(null);
  useEffect(() => {
    if (!search.trim() || query.isLoading || !Array.isArray(query.data)) return;
    const key = `${tag ?? ""}\u0000${search}`;
    if (lastMeasured.current === key) return;
    lastMeasured.current = key;
    captureTelemetryEvent("docs search used", {
      result_count: query.data.length,
    });
  }, [search, tag, query.isLoading, query.data]);
  return (
    <SearchDialog {...props} search={search} onSearchChange={setSearch} isLoading={query.isLoading}>
      <SearchDialogOverlay />
      <SearchDialogContent>
        <SearchDialogHeader>
          <SearchDialogInput placeholder="Search SocialFlow.ai docs..." />
          <SearchDialogClose />
        </SearchDialogHeader>
        <TagsList
          className="docs-search-filters"
          role="group"
          aria-label="Search sections"
          tag={tag}
          onTagChange={setTag}
        >
          <button type="button" aria-pressed={!tag} onClick={() => setTag(undefined)}>
            All docs
          </button>
          {documentationSections.map(({ id, label }) => (
            <TagsListItem key={id} value={id} tabIndex={0} aria-pressed={tag === id}>
              {label}
            </TagsListItem>
          ))}
        </TagsList>
        <SearchDialogList items={query.data !== "empty" ? query.data : null} />
      </SearchDialogContent>
    </SearchDialog>
  );
}
