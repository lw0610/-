"use client";

import React from "react";
import { cn } from "@/lib/utils";
import CollapsibleSection from "./CollapsibleSection";

interface ListDetailLayoutProps {
  list: React.ReactNode;
  detail: React.ReactNode;
  className?: string;
  listClassName?: string;
  detailClassName?: string;
  stickyList?: boolean;
  listTitle?: string;
  listSectionId?: string;
}

export default function ListDetailLayout({
  list,
  detail,
  className,
  listClassName,
  detailClassName,
  stickyList = false,
  listTitle = "目录",
  listSectionId,
}: ListDetailLayoutProps) {
  return (
    <div
      id={listSectionId}
      className={cn(
        listSectionId && "settings-anchor-target",
        "grid grid-cols-1 overflow-hidden rounded border border-zinc-200 lg:grid-cols-[216px,minmax(0,1fr)] xl:grid-cols-[224px,minmax(0,1fr)] dark:border-zinc-800",
        className,
      )}
    >
      <div className="block border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900 lg:hidden">
        <CollapsibleSection
          title={listTitle}
          defaultOpen={false}
          sectionId={listSectionId}
          className="rounded-none border-0 shadow-none"
        >
          <div className={cn("py-1", listClassName)}>
            {list}
          </div>
        </CollapsibleSection>
      </div>
      <div
        className={cn(
          "min-h-0 hidden border-b border-zinc-200 bg-white p-1.5 dark:border-zinc-800 dark:bg-zinc-900 lg:block lg:border-b-0 lg:border-r lg:max-h-[calc(100dvh-10rem)] lg:overflow-y-auto",
          stickyList && "lg:sticky lg:top-0 lg:self-start",
          listClassName,
        )}
      >
        {list}
      </div>
      <div
        className={cn(
          "min-h-0 bg-[#fafafa] p-2.5 dark:bg-zinc-950",
          detailClassName,
        )}
      >
        {detail}
      </div>
    </div>
  );
}
