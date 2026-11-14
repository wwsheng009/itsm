
import React from 'react';
import { Card, Col, Row } from 'antd';
import styles from './BusinessStatsGrid.module.css';

export type BusinessStatsTone = 'blue' | 'orange' | 'green' | 'purple' | 'cyan' | 'red';

interface BusinessStatsItem {
  label: string;
  value: React.ReactNode;
  icon: React.ReactNode;
  tone: BusinessStatsTone;
}

interface BusinessStatsGridProps {
  items: BusinessStatsItem[];
  className?: string;
  loading?: boolean;
}

const toneClassName: Record<BusinessStatsTone, string> = {
  blue: styles.toneBlue,
  orange: styles.toneOrange,
  green: styles.toneGreen,
  purple: styles.tonePurple,
  cyan: styles.toneCyan,
  red: styles.toneRed,
};

export const BusinessStatsGrid: React.FC<BusinessStatsGridProps> = ({
  items,
  className = '',
  loading = false,
}) => {
  if (loading) {
    return (
      <div className={`mb-6 ${className}`}>
        <Row gutter={[16, 16]} align="stretch">
          {Array.from({ length: 4 }).map((_, index) => (
            <Col key={index} xs={24} sm={12} md={6} lg={6} className="flex">
              <Card loading className="w-full rounded-lg shadow-sm" />
            </Col>
          ))}
        </Row>
      </div>
    );
  }

  return (
    <div className={`mb-6 ${className}`}>
      <Row gutter={[16, 16]} align="stretch">
        {items.map((item, index) => {
          const toneClass = toneClassName[item.tone];

          return (
            <Col key={`${item.label}-${index}`} xs={24} sm={12} md={6} lg={6} className="flex">
              <Card
                className={`w-full text-center shadow-sm hover:shadow-lg transition-all duration-300 ${styles.card} ${toneClass}`}
                styles={{ body: { padding: '16px' } }}
              >
                <div
                  className={`mx-auto mb-3 h-10 w-10 rounded-lg flex items-center justify-center ${styles.icon}`}
                >
                  {item.icon}
                </div>
                <div className={`${styles.value} text-2xl font-bold mb-1`}>{item.value}</div>
                <div className={`${styles.label} font-medium text-xs`}>{item.label}</div>
              </Card>
            </Col>
          );
        })}
      </Row>
    </div>
  );
};

export default BusinessStatsGrid;
