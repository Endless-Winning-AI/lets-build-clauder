import { expect, test } from 'claude-code/testing'
import type { On } from 'claude-code'

const STATUS = { type: 'status', slot: 2, device: { connected: true, battery: 61, charging: false }, waiting: 1 }

const ATTACHED = { type: 'attached', session: 's1', socket: '/run/lets-build-clauder/daemon.sock' }

/** Lets the module's pending promises run. */
async function settle() {
  for (let i = 0; i < 1000; i++) {
    await Promise.resolve()
  }
}

/**
 * Stands in for the daemon: the attach process relays `messages` and then
 * whatever `relay` is given, one per line, until `detach`. Records each
 * state the plugin sends it.
 */
function fakeDaemon(on: On, messages: object[], answers: ('allow' | 'deny' | null)[] = []) {
  const sent: string[] = []
  const queue = [...messages]
  let wake = () => {}
  let isDetached = false
  on('http.fetch', (_, e) => {
    expect(e.init?.socketPath).toBe(ATTACHED.socket)
    if (e.url === 'http://lets-build-clauder/sessions/s1/ask') {
      const answer = answers.shift() ?? null
      return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify({ answer }) } }
    }
    expect(e.url).toBe('http://lets-build-clauder/sessions/s1/state')
    sent.push((JSON.parse(e.init?.body ?? '{}') as { state: string }).state)
    return { value: { status: 204, ok: true, headers: {}, text: '' } }
  })
  on('process.spawn', async function* () {
    while (!isDetached) {
      const next = queue.shift()
      if (next === undefined) {
        await new Promise<void>(resolve => (wake = resolve))
        continue
      }
      yield { stream: 'stdout' as const, text: `${JSON.stringify(next)}\n` }
    }
    return { value: { code: 0, signal: null } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  return {
    sent,
    relay: (m: object) => {
      queue.push(m)
      wake()
    },
    detach: () => {
      isDetached = true
      wake()
    },
  }
}

const START = { cwd: '/tmp/project', surface: 'terminal', isInteractive: true } as const

test('reports each phase of a turn, and shows the pad under the prompt', async ($, on) => {
  const daemon = fakeDaemon(on, [ATTACHED, STATUS])
  on('turn.start', () => ({ turnId: 't1' }))
  on('turn.complete', () => ({ text: 'done' }))
  on('classic.PermissionRequest', () => ({}))
  let tail: string | undefined
  on('ui.render', ($, e) => {
    tail = (e.props as { tail?: string }).tail
    const { Text } = $.ui.resolve(e)
    return h(Text, null, '? for shortcuts') as never
  })

  await $.session.start(START)
  await $.turn.start({ turnId: 't1' } as never)
  await $.classic.PermissionRequest({ tool_name: 'Bash', tool_input: { command: 'ls' } } as never)
  await $.turn.complete({ turnId: 't1', reason: 'answer', answer: 'done', durationMs: 1, isAborted: false } as never)
  await settle()
  expect(daemon.sent).toEqual(['working', 'awaiting', 'unread'])

  const props = { isDraft: false, isWorking: false, hint: '? for shortcuts' }
  await $.ui.mount({ plugin: 'lets-build-clauder', surface: 'terminal', component: 'PromptHint', props } as never)
  expect(tail).toBe('key 3 · 🔋61% · 1 waiting')
  daemon.detach()
})

test('sends its state again when the daemon reconnects', async ($, on) => {
  const daemon = fakeDaemon(on, [ATTACHED, STATUS])
  on('turn.start', () => ({ turnId: 't1' }))

  await $.session.start(START)
  await $.turn.start({ turnId: 't1' } as never)
  await settle()
  daemon.relay(ATTACHED)
  await settle()
  expect(daemon.sent).toEqual(['working', 'working'])
  daemon.detach()
})

test('carries out what the pad asks of this session', async ($, on) => {
  const daemon = fakeDaemon(on, [
    ATTACHED,
    STATUS,
    { type: 'action', action: 'command', command: 'fast', args: '' },
    { type: 'action', action: 'effort-up' },
    { type: 'action', action: 'model-next' },
    { type: 'action', action: 'model-prev' },
    { type: 'action', action: 'submit' },
  ])
  const commands: string[] = []
  const submitted: string[] = []
  on('command.run', (_, e) => {
    commands.push(`/${e.command} ${e.args}`.trim())
    return { text: '' }
  })
  on('prompt.read', () => ({ value: { text: 'fix the bug', cursor: 11 } }))
  on('prompt.fill', () => ({ isFilled: true }))
  on('prompt.submit', (_, e) => {
    submitted.push(e.text)
    return { text: e.text }
  })
  on('ui.toast', () => ({ value: undefined }))

  await $.session.start(START)
  await settle()

  // The model steps from Opus: next is Fable, and back is Opus.
  expect(commands).toEqual(['/fast', '/effort xhigh', '/model claude-fable-5-1', '/model claude-opus-5-5'])
  expect(submitted).toEqual(['fix the bug'])
  daemon.detach()
})

test('answers a permission prompt from the pad', async ($, on) => {
  const daemon = fakeDaemon(on, [ATTACHED, STATUS], ['allow', 'deny', null])
  let reachedDialog = 0
  on('classic.PermissionRequest', () => {
    reachedDialog++
    return {}
  })
  await $.session.start(START)
  await settle()

  const ask = { tool_name: 'Bash', tool_input: { command: 'ls' } } as never
  expect(await $.classic.PermissionRequest(ask)).toEqual({ decision: { behavior: 'allow' } })
  expect((await $.classic.PermissionRequest(ask)) as object).toMatchObject({ decision: { behavior: 'deny' } })
  // Unanswered from the pad, the prompt is left to the app.
  await $.classic.PermissionRequest(ask)
  expect(reachedDialog).toBe(1)
  daemon.detach()
})
