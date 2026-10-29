'use client'
import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
export default function LegacyFlowsPage() { const router=useRouter();useEffect(()=>{const query=new URLSearchParams(window.location.search);const guide=query.get('guide')||query.get('flow');router.replace(`/guides${guide?`?guide=${encodeURIComponent(guide)}`:''}`)},[router]);return <p className="p-4 text-sm">正在打开流程…</p> }
