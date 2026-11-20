import { useEffect, useRef } from 'react'
import maplibregl, { type Map as MLMap } from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'

export interface MapViewProps {
  /** GeoJSON FeatureCollection to render as layers (fills + outlines + points). */
  data?: GeoJSON.FeatureCollection | null
  center?: [number, number]
  zoom?: number
  height?: number
  onMapReady?: (map: MLMap) => void
}

/**
 * MapLibre viewer with raster-free demo styling (plain vector background),
 * fitting bounds to the provided features. Works fully offline.
 */
export default function MapView({ data, center = [113.63, 28.19], zoom = 12, height = 420, onMapReady }: MapViewProps) {
  const container = useRef<HTMLDivElement>(null)
  const mapRef = useRef<MLMap | null>(null)

  useEffect(() => {
    if (!container.current || mapRef.current) return
    const map = new maplibregl.Map({
      container: container.current,
      style: {
        version: 8,
        sources: {},
        layers: [
          { id: 'bg', type: 'background', paint: { 'background-color': '#eef3ee' } },
        ],
      },
      center,
      zoom,
    })
    map.addControl(new maplibregl.NavigationControl(), 'top-right')
    mapRef.current = map
    onMapReady?.(map)
    return () => {
      map.remove()
      mapRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const map = mapRef.current
    if (!map || !map.isStyleLoaded()) return
    const src = map.getSource('ctc-features') as maplibregl.GeoJSONSource | undefined
    if (src && data) {
      src.setData(data)
      fitBounds(map, data)
      return
    }
    if (data) {
      map.addSource('ctc-features', { type: 'geojson', data })
      map.addLayer({
        id: 'polygon-fill',
        type: 'fill',
        filter: ['==', ['geometry-type'], 'Polygon'],
        source: 'ctc-features',
        paint: { 'fill-color': '#2e7d32', 'fill-opacity': 0.18 },
      })
      map.addLayer({
        id: 'polygon-line',
        type: 'line',
        filter: ['==', ['geometry-type'], 'Polygon'],
        source: 'ctc-features',
        paint: { 'line-color': '#2e7d32', 'line-width': 2 },
      })
      map.addLayer({
        id: 'points',
        type: 'circle',
        filter: ['==', ['geometry-type'], 'Point'],
        source: 'ctc-features',
        paint: {
          'circle-color': '#d84315',
          'circle-radius': 6,
          'circle-stroke-color': '#fff',
          'circle-stroke-width': 2,
        },
      })
      map.addLayer({
        id: 'lines',
        type: 'line',
        filter: ['==', ['geometry-type'], 'LineString'],
        source: 'ctc-features',
        paint: { 'line-color': '#1565c0', 'line-width': 3 },
      })
      fitBounds(map, data)
    }
  }, [data])

  return <div className="map-container" style={{ height }} ref={container} />
}

function fitBounds(map: MLMap, data: GeoJSON.FeatureCollection) {
  const coords: [number, number][] = []
  const collect = (c: unknown) => {
    if (Array.isArray(c) && typeof c[0] === 'number' && typeof c[1] === 'number') {
      coords.push([c[0] as number, c[1] as number])
    } else if (Array.isArray(c)) {
      c.forEach(collect)
    }
  }
  data.features.forEach((f) => collect((f.geometry as { coordinates: unknown } | null)?.coordinates))
  if (coords.length === 0) return
  const lons = coords.map((c) => c[0])
  const lats = coords.map((c) => c[1])
  map.fitBounds(
    [
      [Math.min(...lons), Math.min(...lats)],
      [Math.max(...lons), Math.max(...lats)],
    ],
    { padding: 48, maxZoom: 16, duration: 600 },
  )
}
