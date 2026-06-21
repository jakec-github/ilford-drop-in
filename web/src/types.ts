export interface RotaShift {
  date: string;
  teamLead: string;
  volunteers: string[];
  hotFood: string;
  collection: string;
}

export interface Rota {
  startDate: string;
  shiftCount: number;
  shifts: RotaShift[];
}
