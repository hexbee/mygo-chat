import { create } from 'zustand'

export type PendingScrollTarget = {
  sessionId: string
  messageId: string
  nonce: number
}

type ChatSearchState = {
  open: boolean
  pendingScroll: PendingScrollTarget | null
}

let scrollNonce = 0

export const useChatSearch = create<ChatSearchState>(() => ({
  open: false,
  pendingScroll: null,
}))

export const openChatSearch = () => {
  useChatSearch.setState({ open: true })
}

export const closeChatSearch = () => {
  useChatSearch.setState({ open: false })
}

export const toggleChatSearch = () => {
  useChatSearch.setState((state) => ({ open: !state.open }))
}

export const requestScrollToMessage = (sessionId: string, messageId: string) => {
  scrollNonce += 1
  useChatSearch.setState({
    pendingScroll: { sessionId, messageId, nonce: scrollNonce },
  })
}

export const clearPendingScroll = () => {
  useChatSearch.setState({ pendingScroll: null })
}
