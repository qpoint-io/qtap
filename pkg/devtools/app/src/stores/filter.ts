// Filter Types (no store, types only)

export type Operator = 
  | 'is' 
  | 'is not' 
  | 'contains' 
  | 'starts with' 
  | 'does not contain' 
  | 'does not start with' 
  | 'ends with' 
  | 'does not end with'

export interface Filter {
  key: string
  operator: Operator
  value: string
}

