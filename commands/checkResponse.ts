import { batchGetFormResponses } from '../services/batchGetFormResponses.js';
import { getFormSheet } from "../services/getFormSheet.js"

export const checkResponse = async (firstShift: string, volunteerID: string) => {
    const formSheet = await getFormSheet(firstShift);
    const response = await batchGetFormResponses(
        [formSheet.find((form) => form.volunteerID === volunteerID)?.formID as string],
      );

    console.log(response);
}
