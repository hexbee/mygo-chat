import { FileService } from '../../mygo.js'

const blobToBase64 = async (blob: Blob): Promise<string> => {
  const buffer = await blob.arrayBuffer()
  const bytes = new Uint8Array(buffer)
  let binary = ''
  const len = bytes.byteLength
  const chunkSize = 8192
  for (let i = 0; i < len; i += chunkSize) {
    const chunk = bytes.subarray(i, Math.min(i + chunkSize, len))
    binary += String.fromCharCode.apply(null, Array.from(chunk))
  }
  return btoa(binary)
}

const base64ToFile = (name: string, b64: string): File => {
  const binary = atob(b64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return new File([bytes], name, { type: 'application/zip' })
}

/** 调起原生系统保存对话框 */
export const nativeSaveFile = async (name: string, blob: Blob): Promise<boolean> => {
  try {
    const b64 = await blobToBase64(blob)
    return await FileService.saveFile(name, b64)
  } catch {
    return false
  }
}

/** 调起原生系统打开文件对话框 */
export const nativeOpenFile = async (): Promise<File | null> => {
  try {
    const res = await FileService.openFile()
    if (!res) {
      return null
    }
    return base64ToFile(res.name, res.content)
  } catch {
    return null
  }
}
