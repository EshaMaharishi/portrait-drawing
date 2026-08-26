// Resource names on the drawing machine and typed DoCommand helpers.
import { Struct, type JsonValue, type Resource } from '@viamrobotics/sdk'

export const RESOURCES = {
  camera: 'camera-1',
  personOnly: 'person-only',
  sketched: 'sketched',
  imageToPoses: 'image-to-poses',
  posesToArm: 'poses-to-arm',
} as const

export interface DrawStatus {
  state: 'idle' | 'fetching' | 'drawing' | 'stopped' | 'complete' | 'error'
  completed: number
  total: number
  error: string
}


type Commandable = Pick<Resource, 'doCommand'>

export async function doCommand<T>(client: Commandable, cmd: Record<string, JsonValue>): Promise<T> {
  const result = await client.doCommand(Struct.fromJson(cmd))
  return result as unknown as T
}

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
