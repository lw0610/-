'use client'

import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'

interface Props {
  content: string
  className?: string
}

export default function MarkdownContent({ content, className }: Props) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      className={cn('space-y-2 text-xs leading-6', className)}
      components={{
        p: ({ children }) => <p className="whitespace-pre-wrap break-words">{children}</p>,
        ul: ({ children }) => <ul className="list-disc space-y-1 pl-5">{children}</ul>,
        ol: ({ children }) => <ol className="list-decimal space-y-1 pl-5">{children}</ol>,
        li: ({ children }) => <li className="break-words">{children}</li>,
        code: ({ children, className: codeClassName }) => {
          const inline = !String(codeClassName || '').includes('language-')
          if (inline) {
            return (
              <code className="rounded bg-zinc-200/80 px-1 py-0.5 text-[12px] dark:bg-zinc-700/70">
                {children}
              </code>
            )
          }
          return (
            <code className="block overflow-x-auto rounded bg-zinc-900 px-3 py-2 font-mono text-[12px] text-zinc-100">
              {children}
            </code>
          )
        },
        pre: ({ children }) => <pre className="my-2 overflow-x-auto">{children}</pre>,
        blockquote: ({ children }) => (
          <blockquote className="border-l-2 border-zinc-300 pl-3 text-zinc-600 dark:border-zinc-600 dark:text-zinc-300">
            {children}
          </blockquote>
        ),
        table: ({ children }) => (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-xs">{children}</table>
          </div>
        ),
        th: ({ children }) => (
          <th className="border border-zinc-300 bg-zinc-100 px-2 py-1 text-left dark:border-zinc-700 dark:bg-zinc-800">
            {children}
          </th>
        ),
        td: ({ children }) => <td className="border border-zinc-300 px-2 py-1 dark:border-zinc-700">{children}</td>,
        a: ({ children, href }) => (
          <a
            href={href}
            target="_blank"
            rel="noreferrer"
            className="text-blue-600 underline decoration-blue-300 underline-offset-2 hover:text-blue-500 dark:text-blue-300"
          >
            {children}
          </a>
        ),
      }}
    >
      {content}
    </ReactMarkdown>
  )
}
