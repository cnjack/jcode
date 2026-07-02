import DemoPlayer from '../DemoPlayer'
import DesktopDemo, { DESKTOP_DEMO } from '../../remotion/DesktopDemo'

export default function DesktopDemoSection() {
  return (
    <DemoPlayer
      component={DesktopDemo}
      durationInFrames={DESKTOP_DEMO.durationInFrames}
      fps={DESKTOP_DEMO.fps}
      width={DESKTOP_DEMO.width}
      height={DESKTOP_DEMO.height}
    />
  )
}
