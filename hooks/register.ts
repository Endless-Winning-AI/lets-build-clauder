import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Hint, Reported, State } from '../types'

type DeviceInfo = {
  connected: boolean
  battery?: number
  charging?: boolean
  /** Why the daemon could not open the pad. */
  problem?: string
}

/** What the daemon sends this session, one JSON object per line. */
type Message =
  | { type: 'attached'; session: string; socket: string }
  | { type: 'status'; slot: number | null; device: DeviceInfo; waiting: number }
  | { type: 'state'; state: State }
  | ({ type: 'action' } & Action)
  | { type: 'notice'; text: string }
  | { type: 'error'; message: string }

/** A control on the pad, carried out through Claude Code. */
type Action =
  | { action: 'command'; command: string; args: string }
  | { action: 'text'; text: string; submit: boolean }
  | { action: 'submit' | 'interrupt' | 'fork' | 'effort-up' | 'effort-down' | 'model-next' | 'model-prev' }

const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max']
const DEFAULT_EFFORT = 'high'
/** What the model key steps through, in order. */
const MODELS = [
  { id: 'claude-opus-5-5', name: 'Opus' },
  { id: 'claude-fable-5-1', name: 'Fable' },
  { id: 'claude-sonnet-5-5', name: 'Sonnet' },
]
const LOW_BATTERY = 15

const session: {
  isActive: boolean
  hasReported: boolean
  state: State
  turnId: string | undefined
  effort: string | undefined
  /** The model the model key last chose here. */
  model: number | undefined
  /** Where the daemon takes this session's state, once attached. */
  daemon: { session: string; socket: string } | undefined
  /** The pad problem last shown, so each is shown once. */
  problem: string | undefined
  /** When the daemon last heard the person is typing here. */
  touched: number
  sent: Promise<unknown>
} = {
  isActive: false,
  hasReported: false,
  state: 'idle',
  turnId: undefined,
  effort: undefined,
  model: undefined,
  daemon: undefined,
  problem: undefined,
  touched: 0,
  sent: Promise.resolve(),
}

/** How often, at most, typing in the prompt is reported. */
const TOUCH_EVERY_MS = 3000

const hint = atom({ plugin: 'lets-build-clauder', key: 'hint' } as const, null as Hint)
const reported = atom({ plugin: 'lets-build-clauder', key: 'reported' } as const, null as Reported | null)

function program($: EngineInterface) {
  return `${$.plugin.root}/bin/lets-build-clauder`
}

function show($: EngineInterface, line: Hint) {
  // Rejected once the module has unloaded, when there is nothing to draw.
  update($, hint, () => line).catch(() => undefined)
}

function remember($: EngineInterface) {
  const value: Reported = { state: session.state, turnId: session.turnId ?? null, effort: session.effort ?? null }
  update($, reported, () => value).catch(() => undefined)
}

/**
 * Tell the daemon this session's state. In order, and never awaited by a
 * turn. With `active`, also that the person just typed here: the daemon
 * pins that session when the send key and an agent key are pressed together.
 */
function report($: EngineInterface, state: State, force = false, active = false) {
  if (!session.isActive || (session.state === state && !force && !active)) {
    return
  }
  session.state = state
  session.hasReported = true
  remember($)
  const daemon = session.daemon
  if (daemon === undefined) {
    return // sent on attaching
  }
  if (active) {
    session.touched = Date.now()
  }
  session.sent = session.sent.then(() =>
    $.http
      .fetch(`http://lets-build-clauder/sessions/${daemon.session}/state`, {
        method: 'POST',
        body: JSON.stringify(active ? { state, active: true } : { state }),
        socketPath: daemon.socket,
      })
      .catch(() => undefined),
  )
}

/** On (re)connecting: send the state again, or after a reload, the one before it. */
async function resync($: EngineInterface) {
  if (session.hasReported) {
    report($, session.state, true)
    return
  }
  const before = await read($, reported).catch(() => null)
  if (before !== null && !session.hasReported) {
    session.turnId = before.turnId ?? undefined
    session.effort = before.effort ?? undefined
    report($, before.state, true)
  }
}

function statusLine(m: { slot: number | null; device: DeviceInfo; waiting: number }) {
  if (!m.device.connected) {
    return m.device.problem === undefined ? 'pad asleep' : 'pad unavailable: run /lets-build-clauder'
  }
  const parts = [m.slot === null ? 'no free key' : `key ${m.slot + 1}`]
  const { battery, charging } = m.device
  if (battery !== undefined) {
    parts.push(`${charging ? '⚡' : battery <= LOW_BATTERY ? '🪫' : '🔋'}${battery}%`)
  }
  if (m.waiting > 0) {
    parts.push(`${m.waiting} waiting`)
  }
  return parts.join(' · ')
}

function stepEffort(current: string | undefined, step: number) {
  const at = EFFORTS.indexOf(current ?? DEFAULT_EFFORT)
  const from = at === -1 ? EFFORTS.indexOf(DEFAULT_EFFORT) : at
  return EFFORTS[Math.min(EFFORTS.length - 1, Math.max(0, from + step))] ?? DEFAULT_EFFORT
}

async function act($: EngineInterface, m: Action) {
  switch (m.action) {
    case 'command':
      await $.command.run({ command: m.command, args: m.args })
      return
    case 'text':
      if (m.submit) {
        void $.prompt.submit({ text: m.text })
      } else {
        await $.prompt.fill({ text: m.text, mode: 'insert' })
      }
      return
    case 'submit': {
      const { text } = await $.prompt.read()
      if (text.trim() !== '') {
        await $.prompt.fill({ text: '', mode: 'replace' })
        void $.prompt.submit({ text })
      }
      return
    }
    case 'interrupt':
      if (session.turnId !== undefined) {
        await $.turn.abort({ turnId: session.turnId })
      }
      return
    case 'effort-up':
    case 'effort-down': {
      const level = stepEffort(session.effort, m.action === 'effort-up' ? 1 : -1)
      session.effort = level
      remember($)
      await $.command.run({ command: 'effort', args: level })
      $.ui.toast(`Effort: ${level}`)
      return
    }
    case 'model-next':
    case 'model-prev': {
      // From Opus, unless this session has stepped already.
      const step = m.action === 'model-next' ? 1 : -1
      const next = ((session.model ?? 0) + step + MODELS.length) % MODELS.length
      session.model = next
      const model = MODELS[next]!
      await $.command.run({ command: 'model', args: model.id })
      $.ui.toast(`Model: ${model.name}`)
      return
    }
    case 'fork': {
      // A copy of the conversation in a terminal of its own, so it gets a key.
      const id = await $.session.id()
      const cwd = await $.session.cwd()
      await $.process.run([program($), 'launch', cwd, 'claude', '--resume', id, '--fork-session'])
      $.ui.toast('Forked into a new terminal')
      return
    }
  }
}

async function receive($: EngineInterface, m: Message) {
  switch (m.type) {
    case 'attached':
      session.daemon = { session: m.session, socket: m.socket }
      await resync($)
      return
    case 'status':
      show($, statusLine(m))
      if (m.device.problem !== undefined && m.device.problem !== session.problem) {
        $.ui.toast(`Let's Build Clauder: ${m.device.problem}`)
      }
      session.problem = m.device.problem
      return
    case 'state':
      // The daemon moved this session itself: its answer was read on screen.
      session.state = m.state
      remember($)
      return
    case 'action':
      await act($, m)
      return
    case 'notice':
      $.ui.toast(m.text)
      return
    case 'error':
      $.ui.log(`lets-build-clauder: ${m.message}`, { to: 'debug' })
      return
  }
}

/** Runs the session's attach process and handles what it relays, for the session's life. */
async function follow($: EngineInterface) {
  let buffered = ''
  let failure = ''
  const attach = $.process.spawn({ argv: [program($), 'attach'] })
  for await (const { stream, text } of attach) {
    if (stream === 'stderr') {
      $.ui.log(`lets-build-clauder: ${text}`, { to: 'debug' })
      failure += text
      continue
    }
    buffered += text
    const lines = buffered.split('\n')
    buffered = lines.pop() ?? ''
    for (const line of lines) {
      try {
        await receive($, JSON.parse(line) as Message)
      } catch (e) {
        $.ui.log(`lets-build-clauder: ${line}: ${e}`, { to: 'debug' })
      }
    }
  }
  session.daemon = undefined
  show($, null)
  if (failure.trim() !== '') {
    // attach stopped for good: the download failed, or the plugin needs a restart.
    $.ui.toast(failure.trim().split('\n').pop() ?? failure)
  }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    $.command
      .register({ name: 'lets-build-clauder', description: "Check the Let's Build Clauder pad setup and show its status" })
      .catch(() => undefined)
    // The Claude desktop app's Code tab starts sessions the SDK way
    // (isInteractive false) but a person is at them all the same.
    let attended = e.isInteractive
    if (!attended) {
      try {
        attended =
          (await $.env.get('CLAUDE_CODE_ENTRYPOINT')) === 'claude-desktop' &&
          (await $.env.get('CLAUDE_CODE_HOST_SESSION_ID')) !== undefined &&
          // Agents a desktop session starts (`claude --print`) inherit its
          // host id; only the session itself takes a key.
          (await $.env.get('CLAUDECODE')) === undefined &&
          (await $.env.get('CLAUDE_CODE_CHILD_SESSION')) === undefined
      } catch {
        attended = false // a host without $.env (the test harness)
      }
    }
    if (attended) {
      session.isActive = true
      // Ending the loop (session exit, plugin reload) ends attach, which frees the key.
      void follow($)
    }
    return started
  })

  on('command.run', { command: 'lets-build-clauder' }, async $ => {
    const { stdout, stderr } = await $.process.run([program($), 'doctor'], { timeoutMs: 60000 })
    return { text: (stdout || stderr).trimEnd() }
  })

  on('turn.start', ($, e, next) => {
    if (e.agentId === undefined) {
      session.turnId = e.turnId
      report($, 'working', false, true) // a prompt was sent: the person is here
    }
    return next(e)
  })

  on('classic.PermissionRequest', ($, e, next) => {
    report($, 'awaiting')
    return next(e)
  }).catch(($, e, next) => next(e))

  // A tool call resolves once its permission prompt is answered.
  on('tool.call', async ($, e, next) => {
    const result = await next(e)
    if (session.state === 'awaiting') {
      report($, 'working')
    }
    return result
  }).catch(($, e, next) => next(e))

  on('turn.complete', ($, e, next) => {
    if (e.agentId === undefined) {
      session.turnId = undefined
      report($, e.reason === 'answer' ? 'unread' : e.reason === 'aborted' ? 'idle' : 'error')
    }
    return next(e)
  })

  // The effort each turn ran at, so the dial steps from the current level.
  on('classic.Stop', ($, e, next) => {
    const level = (e as { effort?: { level?: string } }).effort?.level
    if (level !== undefined) {
      session.effort = level
      remember($)
    }
    return next(e)
  }).catch(($, e, next) => next(e))

  // Typing in the prompt box means the last answer has been read, and that
  // this is the session the person is in.
  on('prompt.edit', ($, e, next) => {
    if (session.state === 'unread' || session.state === 'error') {
      report($, 'idle', false, true)
    } else if (Date.now() - session.touched > TOUCH_EVERY_MS) {
      report($, session.state, false, true)
    }
    return next(e)
  })

  on('ui.render', { component: 'PromptHint' }, async ($, e, next) => {
    const line = await read($, hint)
    return line === null ? next(e) : next({ ...e, props: { ...e.props, tail: line } })
  })
}
