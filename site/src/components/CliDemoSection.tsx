import DemoPlayer from './DemoPlayer'
import CliDemo, { CLI_DEMO } from '../remotion/CliDemo'

export default function CliDemoSection() {
  return (
    <DemoPlayer
      component={CliDemo}
      durationInFrames={CLI_DEMO.durationInFrames}
      fps={CLI_DEMO.fps}
      width={CLI_DEMO.width}
      height={CLI_DEMO.height}
    />
  )
}
