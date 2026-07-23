import DemoPlayer from '../DemoPlayer'
import CloudMeshDemo, { CLOUD_MESH_DEMO } from '../../remotion/CloudMeshDemo'

export default function CloudDemoSection() {
  return (
    <DemoPlayer
      component={CloudMeshDemo}
      durationInFrames={CLOUD_MESH_DEMO.durationInFrames}
      fps={CLOUD_MESH_DEMO.fps}
      width={CLOUD_MESH_DEMO.width}
      height={CLOUD_MESH_DEMO.height}
    />
  )
}
