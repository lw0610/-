import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import ResourceMedia, { ResourceThumbnail } from './ResourceMedia'
import { loadResourcePreview } from './api'
vi.mock('./api', () => ({ loadResourcePreview: vi.fn() }))
vi.mock('@/stores/instanceStore', () => ({useInstanceStore: (selector: (state: unknown) => unknown) => selector({instances: []})}))
const originals = Object.fromEntries(['createObjectURL', 'revokeObjectURL'].map(name => [name, Object.getOwnPropertyDescriptor(URL, name)]))
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  for (const [name, descriptor] of Object.entries(originals)) {
    if (descriptor) Object.defineProperty(URL, name, descriptor)
    else Reflect.deleteProperty(URL, name)
  }
})
it('reloads a regenerated preview even when the resource content digest is unchanged', async () => {
  vi.mocked(loadResourcePreview).mockResolvedValue(new Blob(['image'], {type:'image/png'}))
  const create = vi.fn().mockReturnValueOnce('blob:old').mockReturnValueOnce('blob:new')
  Object.defineProperty(URL, 'createObjectURL', {configurable:true, value:create})
  const revoke = vi.fn()
  Object.defineProperty(URL, 'revokeObjectURL', {configurable:true, value:revoke})
  const resource = {id:'style.river', name:'河流线', digest:'unchanged', preview_status:'ready' as const}
  const view = render(<ResourceThumbnail resource={resource}/>)
  await waitFor(() => expect(screen.getByRole('img')).toHaveAttribute('src','blob:old'))
  // GET after regeneration returns the same identity/content but a fresh object.
  view.rerender(<ResourceThumbnail resource={{...resource}}/>)
  await waitFor(() => expect(screen.getByRole('img')).toHaveAttribute('src','blob:new'))
  expect(revoke).toHaveBeenCalledWith('blob:old')
  view.unmount()
  expect(revoke).toHaveBeenCalledWith('blob:new')
})

it('opens an uncropped high resolution preview with zoom controls and keeps portrait layout proportion', async () => {
  vi.mocked(loadResourcePreview).mockResolvedValue(new Blob(['image'], {type: 'image/png'}))
  Object.defineProperty(URL, 'createObjectURL', {configurable: true, value: vi.fn().mockReturnValue('blob:portrait')})
  Object.defineProperty(URL, 'revokeObjectURL', {configurable: true, value: vi.fn()})
  render(<ResourceMedia resource={{id: 'layout.page', type: 'layout', name: '纵向版面', orientation: 'portrait'}}/>)
  const thumbnail = await screen.findByRole('img', {name: '纵向版面预览'})
  expect(thumbnail.parentElement).toHaveClass('aspect-[1/1.414]')
  fireEvent.click(screen.getByRole('button', {name: '放大纵向版面预览'}))
  const dialog = within(screen.getByRole('dialog'))
  const preview = await dialog.findByRole('img', {name: '纵向版面完整预览'})
  Object.defineProperty(preview, 'naturalWidth', {value: 1600})
  fireEvent.load(preview)
  fireEvent.click(dialog.getByRole('button', {name: '原始大小'}))
  expect(preview).toHaveStyle({width: '1600px', maxWidth: 'none'})
  fireEvent.click(dialog.getByRole('button', {name: '缩小预览'}))
  expect(preview).toHaveStyle({width: '1280px'})
  fireEvent.click(dialog.getByRole('button', {name: '适合窗口'}))
  expect(preview).toHaveStyle({maxWidth: '100%', maxHeight: '100%'})
  fireEvent.click(dialog.getByRole('button', {name: '关闭预览'}))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})
