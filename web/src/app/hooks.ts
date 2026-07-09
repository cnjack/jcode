import { useDispatch, useSelector } from 'react-redux'
import type { RootState, AppDispatch } from './store'

/** Typed dispatch + selector hooks (the standard RTK pattern). */
export const useAppDispatch = () => useDispatch<AppDispatch>()
export const useAppSelector = <T>(selector: (s: RootState) => T): T => useSelector(selector)
