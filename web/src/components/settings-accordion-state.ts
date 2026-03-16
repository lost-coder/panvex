export function toggleAccordionSection(currentSection: string | null, nextSection: string): string | null {
  if (currentSection === nextSection) {
    return null;
  }

  return nextSection;
}
